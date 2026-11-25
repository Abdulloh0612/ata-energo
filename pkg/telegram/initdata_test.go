package telegram

import (
	"strings"
	"testing"
	"time"
)

func TestVerifyInitData(t *testing.T) {
	const data = "auth_date=1700000000&query_id=test&user=%7B%22id%22%3A1234567890123%2C%22username%22%3A%22engineer%22%7D&hash=cea4d210ed26adee3f696b937d062da35cff6cc4e4900759c1df15aac8895f9a"
	const token = "12345:test-token"
	issued := time.Unix(1700000000, 0)
	proof, err := VerifyInitData(data, token, issued)
	if err != nil || proof.User.ID != 1234567890123 || proof.User.Username != "engineer" {
		t.Fatalf("proof=%+v error=%v", proof, err)
	}
	for _, tc := range []struct {
		name, raw, token string
		now              time.Time
	}{
		{"tampered user", strings.Replace(data, "engineer", "director", 1), token, issued},
		{"duplicate field", data + "&query_id=test", token, issued},
		{"another bot", data, "54321:other-token", issued},
		{"expired", data, token, issued.Add(LoginTTL)},
		{"future", data, token, issued.Add(-time.Minute)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := VerifyInitData(tc.raw, tc.token, tc.now); err == nil {
				t.Fatal("invalid proof accepted")
			}
		})
	}
}
