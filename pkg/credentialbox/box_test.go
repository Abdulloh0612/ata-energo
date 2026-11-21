package credentialbox

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
)

func key(octet byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{octet}, 32))
}

func TestEnvelopeRotationAndIntegrity(t *testing.T) {
	old, err := New("one", key(1), "", "")
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("sample partner credential")
	aad := []byte("vendor_accounts:deye:4")
	first, err := old.Seal(plain, aad)
	if err != nil {
		t.Fatal(err)
	}
	second, err := old.Seal(plain, aad)
	if err != nil || bytes.Equal(first, second) || bytes.Contains(first, plain) {
		t.Fatal("expected independent opaque envelopes")
	}
	rotated, err := New("two", key(2), "one", key(1))
	if err != nil {
		t.Fatal(err)
	}
	clear, err := rotated.Open("one", first, aad)
	if err != nil || !bytes.Equal(clear, plain) {
		t.Fatal("previous key did not open envelope")
	}
	sealed, err := rotated.Seal(clear, aad)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = rotated.Open("two", sealed, []byte("vendor_accounts:deye:5")); err == nil {
		t.Fatal("envelope accepted for another account")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err = rotated.Open("two", sealed, aad); err == nil {
		t.Fatal("tampering was accepted")
	}
}

func TestMissingKeyDoesNotAllowWrites(t *testing.T) {
	box, err := New("one", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = box.Seal([]byte("credential"), nil); !errors.Is(err, ErrUnconfigured) {
		t.Fatalf("unexpected error %v", err)
	}
}
