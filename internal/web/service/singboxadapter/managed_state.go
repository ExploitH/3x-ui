package singboxadapter

import (
	"errors"
	"fmt"
	"strings"
)

type ManagedRelayStateUser struct {
	ManagedRelayUser
	Enabled bool `json:"enabled"`
}

type ManagedRelayState struct {
	Version    int                     `json:"version"`
	InboundTag string                  `json:"inboundTag"`
	ListenPort int                     `json:"listenPort"`
	Exits      []ManagedRelayExit      `json:"exits"`
	Users      []ManagedRelayStateUser `json:"users"`
}

func (s ManagedRelayState) ActiveFragment() (RelayFragment, error) {
	if err := s.validate(); err != nil {
		return RelayFragment{}, err
	}
	active := make([]ManagedRelayUser, 0, len(s.Users))
	for _, user := range s.Users {
		if user.Enabled {
			active = append(active, user.ManagedRelayUser)
		}
	}
	return BuildManagedRelayFragment(RelayFragmentInput{
		InboundTag: s.InboundTag,
		ListenPort: s.ListenPort,
		Users:      active,
		Exits:      append([]ManagedRelayExit(nil), s.Exits...),
	})
}

func (s ManagedRelayState) SetUserEnabled(email string, enabled bool) (ManagedRelayState, error) {
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
		updated.Users[i].Enabled = enabled
		found = true
	}
	if !found {
		return ManagedRelayState{}, fmt.Errorf("managed relay user %q not found", email)
	}
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
			updated.Users[i] = ManagedRelayStateUser{ManagedRelayUser: canonical, Enabled: enabled}
			return updated, updated.validate()
		}
		if strings.TrimSpace(updated.Users[i].UUID) == canonical.UUID {
			return ManagedRelayState{}, fmt.Errorf("managed relay uuid already belongs to %q", updated.Users[i].Email)
		}
	}
	updated.Users = append(updated.Users, ManagedRelayStateUser{ManagedRelayUser: canonical, Enabled: enabled})
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
	return updated, nil
}

func (s ManagedRelayState) validate() error {
	if err := s.validateBase(); err != nil {
		return err
	}
	seenEmail := make(map[string]struct{}, len(s.Users))
	seenUUID := make(map[string]struct{}, len(s.Users))
	allUsers := make([]ManagedRelayUser, 0, len(s.Users))
	for _, stateUser := range s.Users {
		user, err := normalizeManagedRelayUser(stateUser.ManagedRelayUser)
		if err != nil {
			return err
		}
		if user.Email != stateUser.Email || user.UUID != stateUser.UUID || user.ExitTag != stateUser.ExitTag {
			return fmt.Errorf("managed relay user %q is not canonicalized", stateUser.Email)
		}
		if _, exists := seenEmail[user.Email]; exists {
			return fmt.Errorf("duplicate managed relay user %q", user.Email)
		}
		if _, exists := seenUUID[user.UUID]; exists {
			return fmt.Errorf("duplicate managed relay uuid for %q", user.Email)
		}
		if !managedRelayExitExists(s.Exits, user.ExitTag) {
			return fmt.Errorf("user %q references unknown exit %q", user.Email, user.ExitTag)
		}
		seenEmail[user.Email] = struct{}{}
		seenUUID[user.UUID] = struct{}{}
		allUsers = append(allUsers, user)
	}
	_, err := BuildManagedRelayFragment(RelayFragmentInput{InboundTag: s.InboundTag, ListenPort: s.ListenPort, Users: allUsers, Exits: s.Exits})
	return err
}

func (s ManagedRelayState) validateBase() error {
	if s.Version != 1 {
		return fmt.Errorf("unsupported managed relay state version %d", s.Version)
	}
	if strings.TrimSpace(s.InboundTag) == "" || s.ListenPort < 1 || s.ListenPort > 65535 {
		return errors.New("managed relay inbound tag and port are required")
	}
	if _, err := BuildManagedRelayFragment(RelayFragmentInput{InboundTag: s.InboundTag, ListenPort: s.ListenPort, Exits: s.Exits}); err != nil {
		return err
	}
	return nil
}

func normalizeManagedRelayUser(user ManagedRelayUser) (ManagedRelayUser, error) {
	user.Email = canonicalRelayEmail(user.Email)
	user.UUID = strings.TrimSpace(user.UUID)
	user.ExitTag = strings.TrimSpace(user.ExitTag)
	if user.Email == "" || user.UUID == "" || user.ExitTag == "" {
		return ManagedRelayUser{}, errors.New("managed relay user email, uuid, and exit tag are required")
	}
	return user, nil
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
