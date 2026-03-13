package security

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
)

// PublicJWK is a minimal public key representation for clients.
type PublicJWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Kid string `json:"kid"`
	X   string `json:"x"`
	Use string `json:"use,omitempty"`
	Alg string `json:"alg,omitempty"`
}

// Signer handles Ed25519 signing for manifests.
type Signer struct {
	keyID string
	pub   ed25519.PublicKey
	priv  ed25519.PrivateKey
}

func NewSigner(keyID string, seedB64 string) (*Signer, error) {
	if keyID == "" {
		return nil, errors.New("key id is required")
	}
	if seedB64 == "" {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		return &Signer{keyID: keyID, pub: pub, priv: priv}, nil
	}

	seed, err := base64.StdEncoding.DecodeString(seedB64)
	if err != nil {
		return nil, err
	}
	if len(seed) != ed25519.SeedSize {
		return nil, errors.New("seed must decode to 32 bytes")
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	return &Signer{keyID: keyID, pub: pub, priv: priv}, nil
}

func (s *Signer) KeyID() string {
	return s.keyID
}

func (s *Signer) PublicKeyBase64() string {
	return base64.StdEncoding.EncodeToString(s.pub)
}

func (s *Signer) PublicKeyBase64URL() string {
	return base64.RawURLEncoding.EncodeToString(s.pub)
}

func (s *Signer) PublicJWK() PublicJWK {
	return PublicJWK{
		Kty: "OKP",
		Crv: "Ed25519",
		Kid: s.keyID,
		X:   s.PublicKeyBase64URL(),
		Use: "sig",
		Alg: "EdDSA",
	}
}

func (s *Signer) Sign(payload []byte) string {
	sig := ed25519.Sign(s.priv, payload)
	return base64.StdEncoding.EncodeToString(sig)
}

func Verify(publicKeyB64 string, payload []byte, signatureB64 string) bool {
	pub, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return false
	}
	if len(pub) != ed25519.PublicKeySize {
		return false
	}
	if len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(pub), payload, sig)
}
