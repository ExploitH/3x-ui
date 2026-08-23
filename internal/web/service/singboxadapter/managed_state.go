package singboxadapter

import (
	"errors"
	"fmt"
	"strings"
)

type ManagedRelayStateUser struct {
	ManagedRelayUser
	Enabled      bool `json:"enabled"`
	AdminEnabled bool `json:"adminEnabled"`
	QuotaBlocked bool `json:"quotaBlocked"`
	Detached     bool `json:"detached"`
}

type ManagedRelayState struct {
	Version         int                     `json:"version"`
	InboundTag      string                  `json:"inboundTag"`
	ListenPort      int                     `json:"listenPort"`
	CertificatePath string                  `json:"certificatePath"`
	KeyPath         string                  `json:"keyPath"`
	Exits           []ManagedRelayExit      `json:"exits"`
	Users           []ManagedRelayStateUser `json:"users"`
}

func (s ManagedRelayState) ActiveFragment() (RelayFragment, error) {
	if err := s.validate(); err != nil {
		return RelayFragment{}, err
	}
	active := make([]ManagedRelayUser, 0, len(s.Users))
	for _, user := range s.Users {
		if user.Enabled && !user.Detached {
			active = append(active, user.ManagedRelayUser)
		}
	}
	return BuildManagedRelayFragment(RelayFragmentInput{
		InboundTag: s.InboundTag, ListenPort: s.ListenPort,
		CertificatePath: s.CertificatePath, KeyPath: s.KeyPath,
		Users: active, Exits: append([]ManagedRelayExit(nil), s.Exits...),
	})
}

func (s ManagedRelayState) SetUserEnabled(email string, enabled bool) (ManagedRelayState, error) {
	return s.SetUserAdminEnabled(email, enabled)
}

// SetUserQuotaBlocked changes only the physical-node quota overlay. It never
// re-enables a user that is administratively disabled.
func (s ManagedRelayState) SetUserQuotaBlocked(email string, blocked bool) (ManagedRelayState, error) {
	key := canonicalRelayEmail(email)
	if key == "" {
		return ManagedRelayState{}, errors.New("managed relay user email is required")
	}
	if err := s.validate(); err != nil {
		return ManagedRelayState{}, err
	}
	updated := cloneManagedRelayState(s)
	found := false
	for i := range updated.Users {
		if canonicalRelayEmail(updated.Users[i].Email) != key {
			continue
		}
		adminEnabled, _, err := managedRelayUserFlags(updated.Users[i])
		if err != nil {
			return ManagedRelayState{}, err
		}
		updated.Users[i].AdminEnabled = adminEnabled
		updated.Users[i].QuotaBlocked = blocked
		updated.Users[i].Enabled = adminEnabled && !blocked && !updated.Users[i].Detached
		found = true
	}
	if !found {
		return ManagedRelayState{}, fmt.Errorf("managed relay user %q not found", email)
	}
	updated.Version = 2
	return updated, nil
}

// SetUserAdminEnabled changes the operator/admin desired state. A quota block
// remains in force until SetUserQuotaBlocked(..., false) is called.
func (s ManagedRelayState) SetUserAdminEnabled(email string, enabled bool) (ManagedRelayState, error) {
	key := canonicalRelayEmail(email)
	if key == "" {
		return ManagedRelayState{}, errors.New("managed relay user email is required")
	}
	if err := s.validate(); err != nil {
		return ManagedRelayState{}, err
	}
	updated := cloneManagedRelayState(s)
	found := false
	for i := range updated.Users {
		if canonicalRelayEmail(updated.Users[i].Email) != key {
			continue
		}
		_, quotaBlocked, err := managedRelayUserFlags(updated.Users[i])
		if err != nil {
			return ManagedRelayState{}, err
		}
		updated.Users[i].AdminEnabled = enabled
		updated.Users[i].QuotaBlocked = quotaBlocked
		updated.Users[i].Enabled = enabled && !quotaBlocked && !updated.Users[i].Detached
		found = true
	}
	if !found {
		return ManagedRelayState{}, fmt.Errorf("managed relay user %q not found", email)
	}
	updated.Version = 2
	return updated, nil
}

func (s ManagedRelayState) UpsertUser(user ManagedRelayUser, enabled bool) (ManagedRelayState, error) {
	if s.Version == 0 {
		s.Version = 1
	}
	if err := s.validateBase(); err != nil {
		return ManagedRelayState{}, err
	}
	canonical, err := normalizeManagedRelayUser(user)
	if err != nil {
		return ManagedRelayState{}, err
	}
	if !managedRelayExitExists(s.Exits, canonical.ExitTag) {
		return ManagedRelayState{}, fmt.Errorf("managed relay exit %q not found", canonical.ExitTag)
	}
	updated := cloneManagedRelayState(s)
	for i := range updated.Users {
		if canonicalRelayEmail(updated.Users[i].Email) == canonical.Email {
			_, quotaBlocked, err := managedRelayUserFlags(updated.Users[i])
			if err != nil {
				return ManagedRelayState{}, err
			}
			updated.Users[i] = ManagedRelayStateUser{
				ManagedRelayUser: canonical,
				Enabled:          enabled && !quotaBlocked,
				AdminEnabled:     enabled,
				QuotaBlocked:     quotaBlocked,
				Detached:         false,
			}
			updated.Version = 2
			return updated, updated.validate()
		}
		if strings.TrimSpace(updated.Users[i].Password) == canonical.Password {
			return ManagedRelayState{}, fmt.Errorf("managed relay password already belongs to %q", updated.Users[i].Email)
		}
	}
	updated.Users = append(updated.Users, ManagedRelayStateUser{
		ManagedRelayUser: canonical,
		Enabled:          enabled,
		AdminEnabled:     enabled,
	})
	updated.Version = 2
	return updated, updated.validate()
}

func (s ManagedRelayState) RemoveUser(email string) (ManagedRelayState, error) {
	key := canonicalRelayEmail(email)
	if key == "" {
		return ManagedRelayState{}, errors.New("managed relay user email is required")
	}
	if err := s.validate(); err != nil {
		return ManagedRelayState{}, err
	}
	updated := cloneManagedRelayState(s)
	kept := updated.Users[:0]
	removed := false
	for _, user := range updated.Users {
		if canonicalRelayEmail(user.Email) == key {
			removed = true
			continue
		}
		kept = append(kept, user)
	}
	if !removed {
		return ManagedRelayState{}, fmt.Errorf("managed relay user %q not found", email)
	}
	updated.Users = kept
	updated.Version = 2
	return updated, nil
}

// DetachUser removes a client from this managed inbound while retaining its
// canonical credentials for a later re-attach or full client delete.
func (s ManagedRelayState) DetachUser(email string) (ManagedRelayState, error) {
	key := canonicalRelayEmail(email)
	if key == "" {
		return ManagedRelayState{}, errors.New("managed relay user email is required")
	}
	if err := s.validate(); err != nil {
		return ManagedRelayState{}, err
	}
	updated := cloneManagedRelayState(s)
	for i := range updated.Users {
		if canonicalRelayEmail(updated.Users[i].Email) != key {
			continue
		}
		updated.Users[i].Detached = true
		updated.Users[i].Enabled = false
		updated.Version = 2
		return updated, updated.validate()
	}
	return ManagedRelayState{}, fmt.Errorf("managed relay user %q not found", email)
}

func (s ManagedRelayState) validate() error {
	if err := s.validateBase(); err != nil {
		return err
	}
	seenEmail := make(map[string]struct{}, len(s.Users))
	seenPassword := make(map[string]struct{}, len(s.Users))
	allUsers := make([]ManagedRelayUser, 0, len(s.Users))
	for _, stateUser := range s.Users {
		user, err := normalizeManagedRelayUser(stateUser.ManagedRelayUser)
		if err != nil {
			return err
		}
		if user.Email != stateUser.Email || user.Password != stateUser.Password || user.ExitTag != stateUser.ExitTag {
			return fmt.Errorf("managed relay user %q is not canonicalized", stateUser.Email)
		}
		adminEnabled, quotaBlocked, err := managedRelayUserFlags(stateUser)
		if err != nil {
			return err
		}
		if s.Version >= 2 && stateUser.Enabled != (adminEnabled && !quotaBlocked && !stateUser.Detached) {
			return fmt.Errorf("managed relay user %q has inconsistent enabled state", stateUser.Email)
		}
		if _, exists := seenEmail[user.Email]; exists {
			return fmt.Errorf("duplicate managed relay user %q", user.Email)
		}
		if _, exists := seenPassword[user.Password]; exists {
			return fmt.Errorf("duplicate managed relay password for %q", user.Email)
		}
		if !managedRelayExitExists(s.Exits, user.ExitTag) {
			return fmt.Errorf("user %q references unknown exit %q", user.Email, user.ExitTag)
		}
		seenEmail[user.Email] = struct{}{}
		seenPassword[user.Password] = struct{}{}
		allUsers = append(allUsers, user)
	}
	_, err := BuildManagedRelayFragment(RelayFragmentInput{InboundTag: s.InboundTag, ListenPort: s.ListenPort, CertificatePath: s.CertificatePath, KeyPath: s.KeyPath, Users: allUsers, Exits: s.Exits})
	return err
}

func (s ManagedRelayState) validateBase() error {
	if s.Version != 1 && s.Version != 2 {
		return fmt.Errorf("unsupported managed relay state version %d", s.Version)
	}
	if strings.TrimSpace(s.InboundTag) == "" || s.ListenPort < 1 || s.ListenPort > 65535 || strings.TrimSpace(s.CertificatePath) == "" || strings.TrimSpace(s.KeyPath) == "" {
		return errors.New("managed relay inbound tag and port are required")
	}
	if _, err := BuildManagedRelayFragment(RelayFragmentInput{InboundTag: s.InboundTag, ListenPort: s.ListenPort, CertificatePath: s.CertificatePath, KeyPath: s.KeyPath, Exits: s.Exits}); err != nil {
		return err
	}
	return nil
}

func normalizeManagedRelayUser(user ManagedRelayUser) (ManagedRelayUser, error) {
	user.Email = canonicalRelayEmail(user.Email)
	user.Password = strings.TrimSpace(user.Password)
	user.ExitTag = strings.TrimSpace(user.ExitTag)
	if user.Email == "" || user.Password == "" || user.ExitTag == "" {
		return ManagedRelayUser{}, errors.New("managed relay user email, password, and exit tag are required")
	}
	return user, nil
}

func managedRelayUserFlags(user ManagedRelayStateUser) (adminEnabled, quotaBlocked bool, err error) {
	// Version 1 had only Enabled. Interpret legacy enabled users as
	// administratively enabled and legacy disabled users as administratively
	// disabled; neither interpretation can accidentally resurrect an admin block.
	if !user.AdminEnabled && !user.QuotaBlocked {
		return user.Enabled, false, nil
	}
	adminEnabled = user.AdminEnabled
	quotaBlocked = user.QuotaBlocked
	if user.Enabled != (adminEnabled && !quotaBlocked && !user.Detached) {
		return false, false, fmt.Errorf("managed relay user %q has inconsistent disable flags", user.Email)
	}
	return adminEnabled, quotaBlocked, nil
}

func canonicalRelayEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func managedRelayExitExists(exits []ManagedRelayExit, tag string) bool {
	for _, exit := range exits {
		if exit.Tag == tag {
			return true
		}
	}
	return false
}

func cloneManagedRelayState(s ManagedRelayState) ManagedRelayState {
	clone := s
	clone.Exits = append([]ManagedRelayExit(nil), s.Exits...)
	clone.Users = append([]ManagedRelayStateUser(nil), s.Users...)
	return clone
}
