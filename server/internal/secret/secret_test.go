package secret

import (
	"bytes"
	"testing"
)

func TestAuthenticatedEncryption(t *testing.T) {
	v, _ := New(bytes.Repeat([]byte{1}, 32))
	sealed := v.Seal([]byte("credential"), "room:123")
	if sealed == v.Seal([]byte("credential"), "room:123") {
		t.Fatal("nonce reused")
	}
	got, e := v.Open(sealed, "room:123")
	if e != nil || string(got) != "credential" {
		t.Fatal("decrypt")
	}
	if _, e = v.Open(sealed, "room:456"); e == nil {
		t.Fatal("cross-room ciphertext accepted")
	}
	wrong, _ := New(bytes.Repeat([]byte{2}, 32))
	if _, e = wrong.Open(sealed, "room:123"); e == nil {
		t.Fatal("wrong key accepted")
	}
}
