package security

import (
	"testing"
)

func TestSignAndVerify(t *testing.T) {
	signer, err := NewSigner("k1", "")
	if err != nil {
		t.Fatalf("NewSigner error: %v", err)
	}

	payload := []byte(`{"goal":"checkout"}`)
	sig := signer.Sign(payload)
	if sig == "" {
		t.Fatal("signature was empty")
	}

	if !Verify(signer.PublicKeyBase64(), payload, sig) {
		t.Fatal("expected signature verification to pass")
	}

	if Verify(signer.PublicKeyBase64(), []byte("tampered"), sig) {
		t.Fatal("expected tampered payload verification to fail")
	}
}
