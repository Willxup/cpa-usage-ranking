package test

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"testing"

	"cpa-usage-ranking/internal/auth"
	"cpa-usage-ranking/internal/avatars"
	"cpa-usage-ranking/internal/registration"
)

func TestClosedRegistrationKeepsExistingRetryAndAuthentication(t *testing.T) {
	db, repo, open := setup(t)
	ctx := context.Background()
	secret := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	hash, err := auth.HashCredential(secret)
	if err != nil {
		t.Fatal(err)
	}
	first, err := open.Register(ctx, "source", hash, registration.Profile{DisplayName: "Original"})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := avatars.Load(avatars.BuiltinFS())
	if err != nil {
		t.Fatal(err)
	}
	opts := registration.DefaultOptions()
	opts.NewIdentitiesEnabled = false
	closed, err := registration.NewService(repo, catalog, opts)
	if err != nil {
		t.Fatal(err)
	}
	r, err := closed.Register(ctx, "another source", hash, registration.Profile{DisplayName: "\x00", AvatarID: id(-1)})
	if err != nil || r.Created || r.Client.ID != first.Client.ID || r.Client.DisplayName != "Original" {
		t.Fatalf("closed retry: %+v %v", r, err)
	}
	for _, profile := range []registration.Profile{{}, {DisplayName: "new"}} {
		if _, err := closed.Register(ctx, "source", digest(1), profile); !errors.Is(err, registration.ErrRegistrationClosed) {
			t.Fatalf("new identity accepted when closed: %v", err)
		}
	}
	authenticator := auth.NewAuthenticator(repo)
	client, err := authenticator.Authenticate(ctx, http.Header{"Authorization": {"Bearer " + secret}})
	if err != nil || client.ID != first.Client.ID {
		t.Fatalf("switch broke authentication: %+v %v", client, err)
	}
	if err := repo.SetDisabled(ctx, first.Client.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := closed.Register(ctx, "source", hash, registration.Profile{}); !errors.Is(err, registration.ErrDisabled) {
		t.Fatalf("switch obscured disabled state: %v", err)
	}
	if _, err := authenticator.Authenticate(ctx, http.Header{"Authorization": {"Bearer " + secret}}); !errors.Is(err, auth.ErrClientDisabled) {
		t.Fatalf("disabled authentication: %v", err)
	}
	if count(t, db) != 1 {
		t.Fatal("switch created an identity")
	}
}
