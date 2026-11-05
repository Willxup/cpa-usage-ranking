// Package auth handles client-owned bearer credentials; it never issues secrets.
package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"cpa-usage-ranking/internal/clients"
)

var ErrInvalidCredential = errors.New("invalid client credential")

// HashCredential accepts only the canonical, unpadded base64url encoding of
// exactly 32 bytes. Clients must generate these bytes with a CSPRNG and persist
// the secret before registration; the server cannot prove their entropy source.
func HashCredential(text string) (clients.CredentialHash, error) {
	if len(text) != 43 {
		return clients.CredentialHash{}, ErrInvalidCredential
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(text)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != text {
		return clients.CredentialHash{}, ErrInvalidCredential
	}
	return clients.CredentialHash(sha256.Sum256(decoded)), nil
}

// BearerHash reads a single Authorization header, never URL/body parameters.
// No credential text is retained or included in errors.
func BearerHash(header http.Header) (clients.CredentialHash, error) {
	values := header.Values("Authorization")
	if len(values) != 1 {
		return clients.CredentialHash{}, ErrInvalidCredential
	}
	scheme, text, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return clients.CredentialHash{}, ErrInvalidCredential
	}
	return HashCredential(text)
}
