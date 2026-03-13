package security

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// PublicJWK is a minimal Ed25519 public key representation.
type PublicJWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Kid string `json:"kid"`
	X   string `json:"x"`
	Use string `json:"use,omitempty"`
	Alg string `json:"alg,omitempty"`
}

// Signer handles Ed25519 signatures and compact JWS output.
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

// SignCompactJWS signs payload and returns compact JWS with kid in protected header.
func (s *Signer) SignCompactJWS(payload []byte) (string, error) {
	header := map[string]string{"alg": "EdDSA", "kid": s.keyID, "typ": "JWT"}
	hb, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	headEnc := base64.RawURLEncoding.EncodeToString(hb)
	payloadEnc := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := headEnc + "." + payloadEnc
	sig := ed25519.Sign(s.priv, []byte(signingInput))
	sigEnc := base64.RawURLEncoding.EncodeToString(sig)
	return signingInput + "." + sigEnc, nil
}

// VerifyCompactJWS verifies compact JWS using a JWKS key set and returns payload and kid.
func VerifyCompactJWS(jws string, keys []PublicJWK) ([]byte, string, error) {
	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		return nil, "", errors.New("invalid jws format")
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, "", fmt.Errorf("invalid jws header: %w", err)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, "", fmt.Errorf("invalid jws header json: %w", err)
	}
	if header.Alg != "EdDSA" {
		return nil, "", errors.New("unsupported jws alg")
	}
	if header.Kid == "" {
		return nil, "", errors.New("missing kid")
	}

	var key *PublicJWK
	for i := range keys {
		if keys[i].Kid == header.Kid {
			key = &keys[i]
			break
		}
	}
	if key == nil {
		return nil, "", errors.New("unknown kid")
	}

	pub, err := base64.RawURLEncoding.DecodeString(key.X)
	if err != nil {
		return nil, "", fmt.Errorf("invalid jwk x: %w", err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, "", fmt.Errorf("invalid jws signature: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(parts[0]+"."+parts[1]), sig) {
		return nil, "", errors.New("signature verification failed")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, "", fmt.Errorf("invalid jws payload: %w", err)
	}
	return payload, header.Kid, nil
}
