package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ata-energo/db/repositories"
	"ata-energo/pkg/valueclouds"
	"ata-energo/types"
)

type vcAccounts struct {
	repositories.VendorAccountRepository
	account *types.VendorAccount
}

func (r *vcAccounts) GetByID(context.Context, int64) (*types.VendorAccount, error) {
	a := *r.account
	return &a, nil
}
func (r *vcAccounts) Create(_ context.Context, a *types.VendorAccount) error {
	a.ID = 1
	copy := *a
	r.account = &copy
	return nil
}
func (r *vcAccounts) Update(_ context.Context, a *types.VendorAccount) error {
	copy := *a
	r.account = &copy
	return nil
}
func TestValueCloudsAccountHidesPasswordAndPreservesOmittedValues(t *testing.T) {
	repo := &vcAccounts{}
	s := NewValueCloudsService(repo, nil, nil, nil)
	account, err := s.SaveAccount(context.Background(), 0, types.VendorAccountWrite{Username: strPtr("operator"), Password: strPtr("example-password")})
	if err != nil {
		t.Fatal(err)
	}
	if account.PasswordSHA1 != valueclouds.HashPassword("example-password") || account.PasswordSHA256 != "" {
		t.Fatal("wrong password format")
	}
	encoded, err := json.Marshal(account)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	_ = json.Unmarshal(encoded, &fields)
	if _, ok := fields["PasswordSHA1"]; ok {
		t.Fatal("password serialized")
	}
	if _, ok := fields["password_sha1"]; ok {
		t.Fatal("password serialized")
	}
	old := account.PasswordSHA1
	account, err = s.SaveAccount(context.Background(), account.ID, types.VendorAccountWrite{Name: strPtr("renamed"), Password: strPtr("")})
	if err != nil {
		t.Fatal(err)
	}
	if account.PasswordSHA1 != old || account.Username != "operator" {
		t.Fatal("omitted credentials changed")
	}
}
func TestValueCloudsCatalogIncludesEmptyPlantsAndOrphanDevice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ppr/web/login/login":
			_, _ = w.Write([]byte(`{"code":0,"data":{"token":"t","secret":"s"}}`))
		case "/dgm/api/dgm/itemseybond/getInfoByParams":
			_, _ = w.Write([]byte(`{"code":0,"data":{"total":2,"records":[{"id":10,"itemName":"Plant"},{"id":20,"itemName":"Empty"}]}}`))
		case "/dev/api/auth/valueCloud/dev/listTDeviceInfo":
			if r.URL.Query().Get("deviceStatus") != "" {
				t.Error("offline devices must remain in catalog")
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"total":3,"items":[{"pn":"p","sn":"s1","devcode":1,"devaddr":1,"pid":10,"deviceStatus":1},{"pn":"p","sn":"s2","devcode":1,"devaddr":2,"pid":10,"deviceStatus":0},{"pn":"q","sn":"s3","devcode":1,"devaddr":1,"deviceStatus":1,"deviceName":"Orphan"}]}}`))
		default:
			t.Error("unexpected path", r.URL.Path)
		}
	}))
	defer srv.Close()
	a := &types.VendorAccount{ID: 1, Vendor: types.VendorValueClouds, BaseURL: valueclouds.BaseURL, Username: "a", PasswordSHA1: "h", IsActive: true, ProviderEnabled: true}
	s := NewValueCloudsService(&vcAccounts{account: a}, nil, nil, nil)
	s.cache[1] = cachedValueClouds{"a|h", valueclouds.NewClient(srv.URL, valueclouds.Credentials{Account: "a", PasswordSHA1: "h"}, srv.Client())}
	sites, err := s.FetchSites(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 3 || sites[0].Status != "warning" || sites[1].Status != "empty" || sites[2].Name != "Orphan" {
		t.Fatal(sites)
	}
	a.IsActive = false
	if _, err = s.FetchSites(context.Background(), 1); err == nil {
		t.Fatal("disabled account fetched")
	}
}
func TestValueCloudsPIDHandlesNullWithoutMergingDevices(t *testing.T) {
	a := valueclouds.Device{PN: "p", SN: "s", Address: 1, PID: json.RawMessage("null")}
	b := a
	b.Address = 2
	if ValueCloudsSiteID(a) == ValueCloudsSiteID(b) {
		t.Fatal("orphan identities collided")
	}
}

func (r *vcAccounts) FindByAppAndLogin(_ context.Context, vendor, appID, email, username, mobile string) (*types.VendorAccount, error) {
	if r.account != nil && r.account.Vendor == vendor && r.account.Username == username {
		a := *r.account
		return &a, nil
	}
	return nil, nil
}
func TestValueCloudsPrehashedPassword(t *testing.T) {
	hash := valueclouds.HashPassword("test-password")
	repo := &vcAccounts{}
	s := NewValueCloudsService(repo, nil, nil, nil)
	a, err := s.SaveAccount(context.Background(), 0, types.VendorAccountWrite{Username: strPtr("operator"), PasswordSHA1: &hash})
	if err != nil {
		t.Fatal(err)
	}
	if a.PasswordSHA1 != hash {
		t.Fatal("SHA-1 was hashed twice")
	}
	for _, bad := range []string{"plain-password", "1234", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"} {
		if _, err := s.SaveAccount(context.Background(), 1, types.VendorAccountWrite{PasswordSHA1: &bad}); err == nil {
			t.Fatal("invalid digest accepted")
		}
	}
	if _, err := s.SaveAccount(context.Background(), 1, types.VendorAccountWrite{PasswordSHA1: &hash, Password: strPtr("password")}); err == nil {
		t.Fatal("ambiguous credentials accepted")
	}
}
func TestValueCloudsEnvironmentBootstrapPreservesSavedAccount(t *testing.T) {
	repo := &vcAccounts{}
	s := NewValueCloudsService(repo, nil, nil, nil)
	hash := valueclouds.HashPassword("test-password")
	if err := s.EnsureEnvAccount(context.Background(), "operator", hash); err != nil {
		t.Fatal(err)
	}
	repo.account.Name = "Edited in CRM"
	if err := s.EnsureEnvAccount(context.Background(), "operator", valueclouds.HashPassword("changed")); err != nil {
		t.Fatal(err)
	}
	if repo.account.Name != "Edited in CRM" || repo.account.PasswordSHA1 != hash {
		t.Fatal("bootstrap overwrote saved credentials")
	}
}
