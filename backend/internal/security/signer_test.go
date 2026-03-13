package security

import "testing"

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

func TestCompactJWS(t *testing.T) {
	signer, err := NewSigner("kid-1", "")
	if err != nil {
		t.Fatalf("NewSigner error: %v", err)
	}

	payload := []byte(`{"goal":"note_draft"}`)
	jws, err := signer.SignCompactJWS(payload)
	if err != nil {
		t.Fatalf("sign jws: %v", err)
	}
	if jws == "" {
		t.Fatal("jws was empty")
	}

	out, kid, err := VerifyCompactJWS(jws, []PublicJWK{signer.PublicJWK()})
	if err != nil {
		t.Fatalf("verify jws: %v", err)
	}
	if kid != "kid-1" {
		t.Fatalf("unexpected kid: %s", kid)
	}
	if string(out) != string(payload) {
		t.Fatalf("payload mismatch")
	}

	if _, _, err := VerifyCompactJWS(jws, []PublicJWK{}); err == nil {
		t.Fatal("expected unknown kid error")
	}
}
