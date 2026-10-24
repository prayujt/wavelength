package accounts_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/prayujt/wavelength/internal/accounts"
	"github.com/prayujt/wavelength/internal/state"
)

func TestCredentialsPersistWithoutExposingPasswords(t *testing.T) {
	ctx := context.Background()
	store := &state.Memory{}
	key := bytes.Repeat([]byte{3}, 32)
	service, err := accounts.New(store, key, "admin", "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	password, email := "private-password", "alice@example.test"
	if err := service.Create(ctx, "alice", accounts.Changes{Password: &password, Email: &email}); err != nil {
		t.Fatal(err)
	}
	raw, err := store.Get(ctx, "subsonic-users", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(password)) {
		t.Fatal("plaintext password persisted")
	}
	service, err = accounts.New(store, key, "admin", "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	user, ok, err := service.Verify(ctx, "alice", func(value string) bool { return value == password })
	if err != nil || !ok || user.Username != "alice" {
		t.Fatalf("persisted credentials: %+v %t %v", user, ok, err)
	}
	newPassword := "replacement"
	if err := service.Update(ctx, "alice", accounts.Changes{Password: &newPassword}); err != nil {
		t.Fatal(err)
	}
	_, ok, err = service.Verify(ctx, "alice", func(value string) bool { return value == password })
	if err != nil || ok {
		t.Fatal("old password still accepted")
	}
	if err := service.Delete(ctx, "admin"); !errors.Is(err, accounts.ErrForbidden) {
		t.Fatalf("bootstrap deletion: %v", err)
	}
	if err := service.Update(ctx, "admin", accounts.Changes{Roles: map[string]bool{"adminRole": false}}); err != nil {
		t.Fatal(err)
	}
	admin, err := service.Get(ctx, "admin")
	if err != nil || !admin.Roles["adminRole"] {
		t.Fatal("bootstrap privileges removed")
	}
	rate := 65
	if err := service.Update(ctx, "alice", accounts.Changes{MaxBitRate: &rate}); !errors.Is(err, accounts.ErrInvalid) {
		t.Fatalf("invalid bitrate: %v", err)
	}
}
