package singboxadapter

import (
	"errors"
	"fmt"
	"strings"
)

// ManagedRelayClient is the adapter-facing subset of the existing 3x-ui
// client payload. Hysteria2 uses Auth as its password; Password is accepted as
// a compatibility fallback for callers that use that field.
type ManagedRelayClient struct {
	ID       string `json:"id,omitempty"`
	Password string `json:"password,omitempty"`
	Auth     string `json:"auth,omitempty"`
	Email    string `json:"email"`
	Enable   bool   `json:"enable"`
	ExitTag  string `json:"exitTag,omitempty"`
}

func (p ManagedRelayClient) ToUser(state ManagedRelayState) (ManagedRelayUser, error) {
	email := canonicalRelayEmail(p.Email)
	password := strings.TrimSpace(p.Auth)
	if password == "" {
		password = strings.TrimSpace(p.Password)
	}
	if email == "" || password == "" {
		return ManagedRelayUser{}, errors.New("managed relay client email and auth/password are required")
	}
	exitTag := strings.TrimSpace(p.ExitTag)
	if exitTag == "" {
		for _, existing := range state.Users {
			if canonicalRelayEmail(existing.Email) == email {
				exitTag = existing.ExitTag
				break
			}
		}
	}
	if exitTag == "" {
		if len(state.Exits) != 1 {
			return ManagedRelayUser{}, errors.New("managed relay client exitTag is required when multiple exits exist")
		}
		exitTag = state.Exits[0].Tag
	}
	if !managedRelayExitExists(state.Exits, exitTag) {
		return ManagedRelayUser{}, fmt.Errorf("managed relay exit %q not found", exitTag)
	}
	return ManagedRelayUser{Email: email, Password: password, ExitTag: exitTag}, nil
}
