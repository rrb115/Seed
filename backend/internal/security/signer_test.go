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

func TestPublicJWK(t *testing.T) {
	signer, err := NewSigner("kid-1", "")
	if err != nil {
		t.Fatalf("NewSigner error: %v", err)
	}
	jwk := signer.PublicJWK()
	if jwk.Kid != "kid-1" {
		t.Fatalf("unexpected kid: %s", jwk.Kid)
	}
	if jwk.Kty != "OKP" || jwk.Crv != "Ed25519" {
		t.Fatalf("unexpected jwk type: %#v", jwk)
	}
	if jwk.X == "" {
		t.Fatal("jwk x coordinate is empty")
	}
}
