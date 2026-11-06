package tui

import (
	"errors"
	"fmt"
	"testing"

	"github.com/passbolt/go-passbolt/api"
)

func TestSharesFromPermissions(t *testing.T) {
	users := map[string]api.User{
		"u1": {ID: "u1", Username: "ana@example.com", Profile: &api.Profile{FirstName: "Ana", LastName: "Petrova"}},
		"u2": {ID: "u2", Username: "bob@example.com"},
	}
	groups := map[string]string{"g1": "Ops"}
	perms := []api.Permission{
		{ARO: "User", AROForeignKey: "u2", Type: accessRead},
		{ARO: "Group", AROForeignKey: "g1", Type: accessUpdate},
		{ARO: "User", AROForeignKey: "u1", Type: accessOwner},
		{ARO: "User", AROForeignKey: "0123456789abcdef", Type: accessRead},
		{ARO: "Group", AROForeignKey: "gone", Type: accessRead},
	}
	got := sharesFromPermissions(perms, users, groups)
	want := []Share{
		{Name: "Ana Petrova", Detail: "ana@example.com", Type: accessOwner},
		{Group: true, Name: "Ops", Type: accessUpdate},
		{Name: "bob@example.com", Type: accessRead},
		{Name: "unknown user 01234567", Type: accessRead},
		{Group: true, Name: "unknown group gone", Type: accessRead},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d shares, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("share %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestIsSessionError(t *testing.T) {
	cases := map[error]bool{
		&api.APIError{StatusCode: 401}:                            true,
		&api.APIError{StatusCode: 403}:                            true,
		fmt.Errorf("wrapped: %w", &api.APIError{StatusCode: 401}): true,
		&api.APIError{StatusCode: 404}:                            false,
		&api.APIError{StatusCode: 500}:                            false,
		errors.New("handling MFA callback: restart"):              false,
		nil: false,
	}
	for err, want := range cases {
		if got := isSessionError(err); got != want {
			t.Errorf("isSessionError(%v) = %v, want %v", err, got, want)
		}
	}
}
