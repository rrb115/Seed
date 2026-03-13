package core

import "encoding/json"

// CanonicalManifestBytes returns deterministic bytes used for signing and verification.
// The signed payload is the manifest without the signature field.
func CanonicalManifestBytes(payload ManifestPayload) ([]byte, error) {
	return json.Marshal(payload)
}
