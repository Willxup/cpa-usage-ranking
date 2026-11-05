package auth

import (
	"context"
	"errors"
	"net/http"

	"cpa-usage-ranking/internal/clients"
)

var ErrClientDisabled = errors.New("client is disabled")

type Authenticator struct{ repo *clients.Repository }

func NewAuthenticator(repo *clients.Repository) *Authenticator {
	return &Authenticator{repo: repo}
}

// Authenticate binds a request to the one client selected by its credential.
// Request-supplied IDs and public profile fields are never consulted.
func (a *Authenticator) Authenticate(ctx context.Context, header http.Header) (clients.Client, error) {
	hash, err := BearerHash(header)
	if err != nil {
		return clients.Client{}, err
	}
	client, err := a.repo.ByCredentialHash(ctx, hash)
	if errors.Is(err, clients.ErrNotFound) {
		return clients.Client{}, ErrInvalidCredential
	}
	if err != nil {
		return clients.Client{}, err
	}
	if client.Disabled {
		return clients.Client{}, ErrClientDisabled
	}
	return client, nil
}
