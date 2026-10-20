package agent

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"rasbeacon/internal/api"
	"rasbeacon/internal/config"
)

// Changing the admin password from the System page.

func TestAdminPassword_ChangesWithTheCurrentOne(t *testing.T) {
	e := newClaimedEnv(t, false)
	token, _, err := e.agent.RememberLogin()
	if err != nil {
		t.Fatal(err)
	}

	err = e.agent.ChangeAdminPassword(api.AdminPasswordRequest{CurrentPassword: validClaim.AdminPassword, NewPassword: "plum-harbor-sunrise-7"})
	if err != nil {
		t.Fatal(err)
	}
	if e.agent.VerifyAdminPassword(validClaim.AdminPassword) || !e.agent.VerifyAdminPassword("plum-harbor-sunrise-7") {
		t.Error("the new password should replace the old one")
	}
	// Saved, so it survives a restart.
	var saved config.Secrets
	if err := config.ReadJSON(e.paths.SecretsFile(), &saved); err != nil || saved.AdminPasswordHash == "" {
		t.Fatalf("secrets = %+v, %v", saved, err)
	}
	if saved.HotspotPassword != validClaim.HotspotPassword {
		t.Error("the other secrets must be kept")
	}
	// Every "Stay logged in" ends.
	if e.agent.CheckRememberedLogin(token) {
		t.Error("remembered logins should be forgotten")
	}
}

func TestAdminPassword_Refusals(t *testing.T) {
	tests := []struct {
		name      string
		req       api.AdminPasswordRequest
		wantField string // "" for a wrong current password
	}{
		{"a wrong current password", api.AdminPasswordRequest{CurrentPassword: "nope", NewPassword: "plum-harbor-sunrise-7"}, ""},
		{"a new password too short", api.AdminPasswordRequest{CurrentPassword: validClaim.AdminPassword, NewPassword: "short"}, "newPassword"},
		{"a very common password", api.AdminPasswordRequest{CurrentPassword: validClaim.AdminPassword, NewPassword: "password123"}, "newPassword"},
		{"the same password again", api.AdminPasswordRequest{CurrentPassword: validClaim.AdminPassword, NewPassword: validClaim.AdminPassword}, "newPassword"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newClaimedEnv(t, false)
			err := e.agent.ChangeAdminPassword(tc.req)
			var verr *config.ValidationError
			switch {
			case tc.wantField == "" && !errors.Is(err, ErrWrongPassword):
				t.Errorf("err = %v, want ErrWrongPassword", err)
			case tc.wantField != "" && (!errors.As(err, &verr) || verr.Field != tc.wantField):
				t.Errorf("err = %v, want an error on %s", err, tc.wantField)
			}
			if !e.agent.VerifyAdminPassword(validClaim.AdminPassword) {
				t.Error("a refused change must keep the old password")
			}
		})
	}
}

func TestRoutes_AdminPassword(t *testing.T) {
	e := newClaimedEnv(t, false)
	status, body := e.call(http.MethodPost, "/system/admin-password", api.AdminPasswordRequest{CurrentPassword: "nope", NewPassword: "plum-harbor-sunrise-7"})
	var refused api.ErrorResponse
	if status != http.StatusForbidden || json.Unmarshal(body, &refused) != nil || refused.Field != "password" || refused.Error != "Wrong password." {
		t.Errorf("wrong current password = %d %s", status, body)
	}
	if status, body := e.call(http.MethodPost, "/system/admin-password", api.AdminPasswordRequest{CurrentPassword: validClaim.AdminPassword, NewPassword: "plum-harbor-sunrise-7"}); status != http.StatusNoContent {
		t.Errorf("change = %d %s", status, body)
	}
	if status, _ := e.call(http.MethodPost, "/system/admin-password", "{"); status != http.StatusBadRequest {
		t.Errorf("broken body = %d", status)
	}
}
