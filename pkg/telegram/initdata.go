package telegram

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const LoginTTL = 5 * time.Minute

type Identity struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type Proof struct {
	User      Identity
	Digest    []byte
	ExpiresAt time.Time
}

func VerifyInitData(raw, token string, now time.Time) (Proof, error) {
	invalid := errors.New("invalid Telegram initData")
	if token == "" || len(raw) > 16384 {
		return Proof{}, invalid
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return Proof{}, invalid
	}
	hash, err := hex.DecodeString(values.Get("hash"))
	if err != nil || len(hash) != sha256.Size {
		return Proof{}, invalid
	}
	fields := make([]string, 0, len(values))
	for key, entries := range values {
		if len(entries) != 1 {
			return Proof{}, invalid
		}
		if key != "hash" {
			fields = append(fields, key+"="+entries[0])
		}
	}
	sort.Strings(fields)
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	check := hmac.New(sha256.New, secret.Sum(nil))
	check.Write([]byte(strings.Join(fields, "\n")))
	if !hmac.Equal(hash, check.Sum(nil)) {
		return Proof{}, invalid
	}
	unix, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	issued := time.Unix(unix, 0)
	expires := issued.Add(LoginTTL)
	var user Identity
	if err != nil || !expires.After(now) || issued.After(now.Add(30*time.Second)) ||
		json.Unmarshal([]byte(values.Get("user")), &user) != nil || user.ID <= 0 {
		return Proof{}, invalid
	}
	return Proof{User: user, Digest: hash, ExpiresAt: expires}, nil
}
