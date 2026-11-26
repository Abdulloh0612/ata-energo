package valueclouds

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnvelopeSuccessVariants(t *testing.T) {
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{`{"code":0,"data":{}}`, true},
		{`{"code":200,"data":{}}`, true},
		{`{"code":"200","data":{}}`, true},
		{`{"success":true,"data":{}}`, true},
		{`{"code":200,"success":false}`, false},
		{`{"code":40,"success":true}`, false},
		{`{"code":0.5}`, false},
		{`{"code":"nonsense"}`, false},
		{`{}`, false},
		{`null`, false},
	} {
		t.Run(tc.body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer srv.Close()
			c := NewClient(srv.URL, Credentials{}, srv.Client())
			err := c.request(context.Background(), http.MethodGet, "/test", nil, nil, false, nil)
			if (err == nil) != tc.ok {
				t.Fatal(err)
			}
		})
	}
}

func TestHTTPUnauthorizedHTMLRefreshesReadOnly(t *testing.T) {
	logins, calls := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == loginPath {
			logins++
			_, _ = w.Write([]byte(`{"code":200,"data":{"token":"t","secret":"s","uid":"user-x"}}`))
			return
		}
		calls++
		if calls == 1 {
			http.Error(w, "private upstream text", 401)
			return
		}
		_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, Credentials{"a", "h"}, srv.Client())
	if err := c.Do(context.Background(), http.MethodGet, "/ppe/api/auth/web/querySPDeviceLastData", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if logins != 2 || calls != 2 || c.session.UserID != "user-x" {
		t.Fatal(logins, calls, c.session.UserID)
	}
}

func TestSessionExpiryNeverReplaysControl(t *testing.T) {
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/ppe/api/auth/web/batch/control/item/setUp"},
		{http.MethodGet, "/ppe/api/auth/web/ctrlDevice"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			logins, calls := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == loginPath {
					logins++
					_, _ = w.Write([]byte(`{"code":0,"data":{"token":"t","secret":"s"}}`))
					return
				}
				calls++
				_, _ = w.Write([]byte(`{"code":4}`))
			}))
			defer srv.Close()
			c := NewClient(srv.URL, Credentials{"a", "h"}, srv.Client())
			err := c.Do(context.Background(), tc.method, tc.path, nil, nil, nil)
			var apiErr *APIError
			if !errors.As(err, &apiErr) || calls != 1 || logins != 1 || c.session.Token != "" {
				t.Fatal(err, calls, logins)
			}
		})
	}
}

func TestDeviceAliasesAndIdentityValidation(t *testing.T) {
	d, err := decodeDevice(json.RawMessage(`{"id":123,"pn":"collector","deviceSn":"serial","devcode":"2452","devaddr":"255"}`))
	if err != nil || d.ID != "123" || d.SN != "serial" || d.Address != 255 || d.Status != -1 {
		t.Fatalf("%+v %v", d, err)
	}
	for _, raw := range []string{
		`{"pn":"p","sn":"s","devcode":1}`,
		`{"pn":"p","sn":"s","devaddr":1}`,
		`{"pn":"p","sn":"s","devaddr":true,"devcode":1}`,
	} {
		if _, err := decodeDevice(json.RawMessage(raw)); err == nil {
			t.Fatal(raw)
		}
	}
}

func TestDeviceCatalogRejectsMissingShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == loginPath {
			_, _ = w.Write([]byte(`{"code":0,"data":{"token":"t","secret":"s"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	}))
	defer srv.Close()
	if _, err := NewClient(srv.URL, Credentials{"a", "h"}, srv.Client()).Devices(context.Background()); err == nil {
		t.Fatal("invalid catalog accepted as empty")
	}
}

func TestSignedPathRejectsQueryAndAbsoluteURLs(t *testing.T) {
	c := NewClient("https://api.valueclouds.com", Credentials{}, nil)
	for _, path := range []string{"https://other.invalid/path", "//other.invalid/path", "/path?token=private", "/path#fragment", "relative"} {
		if err := c.request(context.Background(), http.MethodGet, path, nil, nil, false, nil); err == nil {
			t.Fatal(path)
		}
	}
}
