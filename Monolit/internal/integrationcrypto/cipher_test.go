package integrationcrypto

import (
	"encoding/base64"
	"testing"
)

func TestCipherUsesAssociatedData(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	c, err := NewFromBase64(key, 1)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := c.Encrypt([]byte("secret"), "table/row/column/1")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := c.Decrypt(sealed, "table/row/column/1")
	if err != nil || string(plain) != "secret" {
		t.Fatalf("round trip failed: %q %v", plain, err)
	}
	if _, err = c.Decrypt(sealed, "wrong-row"); err == nil {
		t.Fatal("expected associated-data failure")
	}
}
