package valueclouds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestLoginRefreshAndAuthRotation(t *testing.T) {
	logins, requests := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == loginPath {
			logins++
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["account"] != "operator" || body["password"] != HashPassword("password") || body["project"] != "IOT" {
				t.Error("incorrect login body")
			}
			if r.Header.Get("token") != "" || r.Header.Get("sign") != "" || r.Header.Get("Auth") != "" {
				t.Error("login carried old credentials")
			}
			w.Header().Set("Auth", "jwt")
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"token":"token-%d","secret":"key-%d"}}`, logins, logins)
			return
		}
		requests++
		if r.Header.Get("sign") != Sign("/metrics", fmt.Sprintf("key-%d", logins)) || r.Header.Get("token") != fmt.Sprintf("token-%d", logins) {
			t.Error("missing authentication")
		}
		if r.URL.Query().Get("pn") != "a+b" {
			t.Error("query changed")
		}
		if requests == 1 {
			_, _ = w.Write([]byte(`{"code":4}`))
			return
		}
		expected := "jwt"
		if requests > 2 {
			expected = "rotated"
		}
		if r.Header.Get("Auth") != expected {
			t.Error("Auth did not rotate")
		}
		w.Header().Set("Auth", "rotated")
		_, _ = w.Write([]byte(`{"code":0,"data":{"value":12}}`))
	}))
	defer srv.Close()
	client := NewClient(srv.URL, Credentials{"operator", HashPassword("password")}, srv.Client())
	for i := 0; i < 2; i++ {
		var result map[string]int
		if err := client.Do(context.Background(), http.MethodGet, "/metrics", url.Values{"pn": {"a+b"}}, nil, &result); err != nil {
			t.Fatal(err)
		}
		if result["value"] != 12 {
			t.Fatal(result)
		}
	}
	if logins != 2 || requests != 3 {
		t.Fatalf("logins=%d requests=%d", logins, requests)
	}
}
func TestAuthRetryIsBounded(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == loginPath {
			_, _ = w.Write([]byte(`{"code":0,"data":{"token":"t","secret":"s"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":4}`))
	}))
	defer srv.Close()
	cl := NewClient(srv.URL, Credentials{"a", "hashed"}, srv.Client())
	err := cl.Do(context.Background(), http.MethodGet, "/metrics", nil, nil, nil)
	if err == nil || calls != 4 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
func TestDoesNotFollowRedirects(t *testing.T) {
	leaked := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer srv.Close()
	cl := NewClient(srv.URL, Credentials{"a", "hashed"}, srv.Client())
	if cl.Test(context.Background()) == nil || leaked {
		t.Fatal("redirect followed or accepted")
	}
}
func TestErrorsExcludeRemoteMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":40,"message":"sensitive-token-and-password"}`))
	}))
	defer srv.Close()
	err := NewClient(srv.URL, Credentials{"a", "h"}, srv.Client()).Test(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("unexpected error %v", err)
	}
}
func TestDevicesRejectsIncompleteAndRepeatedPages(t *testing.T) {
	for _, repeat := range []bool{false, true} {
		t.Run(map[bool]string{false: "incomplete", true: "repeated"}[repeat], func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == loginPath {
					_, _ = w.Write([]byte(`{"code":0,"data":{"token":"t","secret":"s"}}`))
					return
				}
				if r.URL.Query().Get("page") == "1" || repeat {
					_, _ = w.Write([]byte(`{"code":0,"data":{"total":2,"items":[{"pn":"p","sn":"s","devcode":1,"devaddr":1}]}}`))
				} else {
					_, _ = w.Write([]byte(`{"code":0,"data":{"total":2,"items":[]}}`))
				}
			}))
			defer srv.Close()
			if _, err := NewClient(srv.URL, Credentials{"a", "h"}, srv.Client()).Devices(context.Background()); err == nil {
				t.Fatal("accepted incomplete catalog")
			}
		})
	}
}

// RFC 4231 test case 2 ensures compatibility with standard HMAC-SHA256.
func TestSign(t *testing.T) {
	if Sign("what do ya want for nothing?", "Jefe") != "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843" {
		t.Fatal("incorrect HMAC")
	}
}
