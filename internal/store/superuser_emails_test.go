package store

import (
	"reflect"
	"testing"
)

func TestSuperuserEmails(t *testing.T) {
	s := newStore(t)

	// Active superuser with a verified address — included.
	admin, err := s.InviteUser(ctx, InvitePayload{Username: "admin", IsSuperuser: true})
	if err != nil {
		t.Fatalf("invite admin: %v", err)
	}
	if err := s.UpsertVerifiedEmail(ctx, admin.ID, "admin@example.com"); err != nil {
		t.Fatalf("verify admin email: %v", err)
	}

	// Superuser whose only address is unverified — excluded.
	pending, err := s.InviteUser(ctx, InvitePayload{Username: "pending", IsSuperuser: true})
	if err != nil {
		t.Fatalf("invite pending: %v", err)
	}
	if _, _, err := s.InsertUnverifiedEmail(ctx, pending.ID, "pending@example.com"); err != nil {
		t.Fatalf("insert unverified: %v", err)
	}

	// Regular (non-superuser) user with a verified address — excluded.
	reg, err := s.InviteUser(ctx, InvitePayload{Username: "regular"})
	if err != nil {
		t.Fatalf("invite regular: %v", err)
	}
	if err := s.UpsertVerifiedEmail(ctx, reg.ID, "regular@example.com"); err != nil {
		t.Fatalf("verify regular email: %v", err)
	}

	got, err := s.SuperuserEmails(ctx)
	if err != nil {
		t.Fatalf("SuperuserEmails: %v", err)
	}
	if want := []string{"admin@example.com"}; !reflect.DeepEqual(got, want) {
		t.Errorf("SuperuserEmails = %v, want %v", got, want)
	}
}

func TestSuperuserEmailsEmpty(t *testing.T) {
	s := newStore(t)
	got, err := s.SuperuserEmails(ctx)
	if err != nil {
		t.Fatalf("SuperuserEmails: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no admins, got %v", got)
	}
}
