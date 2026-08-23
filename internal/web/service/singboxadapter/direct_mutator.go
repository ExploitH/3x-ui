package singboxadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// DirectInboundState keeps the original user objects privately so a quota block
// can remove a user from sing-box and a quota reset can restore the exact
// credential object without ever returning credentials through the API.
type DirectInboundState struct {
	Version     int                      `json:"version"`
	InboundTags []string                 `json:"inboundTags"`
	Users       []DirectInboundStateUser `json:"users"`
}

type DirectInboundStateUser struct {
	Email        string                     `json:"email"`
	AdminEnabled bool                       `json:"adminEnabled"`
	QuotaBlocked bool                       `json:"quotaBlocked"`
	RawByTag     map[string]json.RawMessage `json:"rawByTag"`
}

// DirectInboundMutator manages existing direct sing-box inbounds. It is
// deliberately separate from ManagedRelayMutator: direct VLESS/Hysteria2/TUIC
// users have no relay exits and must not be rendered through relay fragments.
type DirectInboundMutator struct {
	mu          sync.Mutex
	StatePath   string
	Config      *ManagedConfigStore
	BinaryPath  string
	InboundTags []string
	Reload      func(context.Context) error
	Health      func(context.Context) error
}

func (m *DirectInboundMutator) Ready(ctx context.Context) error {
	if m == nil || m.Config == nil || strings.TrimSpace(m.StatePath) == "" {
		return errors.New("direct inbound mutator stores are required")
	}
	if strings.TrimSpace(m.BinaryPath) == "" || m.Reload == nil || m.Health == nil {
		return errors.New("direct inbound mutator validation and callbacks are required")
	}
	if len(m.InboundTags) == 0 {
		return errors.New("direct inbound mutator inbound tags are required")
	}
	cfg, err := m.readConfig(ctx)
	if err != nil {
		return err
	}
	_, err = m.loadOrInitializeState(ctx, cfg)
	return err
}

func (m *DirectInboundMutator) AcceptsInboundIDs(ids []int) error {
	if len(ids) == 0 {
		return errors.New("inboundIds do not match direct managed inbounds")
	}
	want := make(map[int]struct{}, len(m.InboundTags))
	for _, tag := range m.InboundTags {
		want[stableInboundID(tag)] = struct{}{}
	}
	seen := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := want[id]; !ok {
			return errors.New("inboundIds do not match direct managed inbounds")
		}
		if _, duplicate := seen[id]; duplicate {
			return errors.New("inboundIds contain duplicates")
		}
		seen[id] = struct{}{}
	}
	return nil
}

func (m *DirectInboundMutator) SetUserQuotaBlocked(ctx context.Context, email string, blocked bool) error {
	return m.mutate(ctx, email, func(user *DirectInboundStateUser) error {
		user.QuotaBlocked = blocked
		return nil
	})
}

func (m *DirectInboundMutator) SetUserAdminEnabled(ctx context.Context, email string, enabled bool) error {
	return m.mutate(ctx, email, func(user *DirectInboundStateUser) error {
		user.AdminEnabled = enabled
		return nil
	})
}

func (m *DirectInboundMutator) readConfig(ctx context.Context) (map[string]any, error) {
	data, err := m.Config.Read(ctx)
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decode direct sing-box config: %w", err)
	}
	return cfg, nil
}

func (m *DirectInboundMutator) loadOrInitializeState(ctx context.Context, cfg map[string]any) (DirectInboundState, error) {
	data, err := os.ReadFile(filepath.Clean(m.StatePath))
	if err == nil {
		var state DirectInboundState
		if err := json.Unmarshal(data, &state); err != nil {
			return DirectInboundState{}, fmt.Errorf("decode direct state: %w", err)
		}
		if err := m.validateState(cfg, state); err != nil {
			return DirectInboundState{}, err
		}
		return state, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return DirectInboundState{}, fmt.Errorf("read direct state: %w", err)
	}
	state, err := m.buildInitialState(cfg)
	if err != nil {
		return DirectInboundState{}, err
	}
	if err := m.saveState(ctx, state); err != nil {
		return DirectInboundState{}, err
	}
	return state, nil
}

func (m *DirectInboundMutator) buildInitialState(cfg map[string]any) (DirectInboundState, error) {
	inbounds, err := configInbounds(cfg)
	if err != nil {
		return DirectInboundState{}, err
	}
	byTag := make(map[string]map[string]json.RawMessage, len(m.InboundTags))
	for _, tag := range m.InboundTags {
		users, ok := inbounds[tag]
		if !ok {
			return DirectInboundState{}, fmt.Errorf("direct inbound %q not found", tag)
		}
		byTag[tag] = users
	}
	var emails []string
	seen := map[string]struct{}{}
	for _, tag := range m.InboundTags {
		for email := range byTag[tag] {
			if _, ok := seen[email]; !ok {
				emails = append(emails, email)
				seen[email] = struct{}{}
			}
		}
	}
	sort.Strings(emails)
	users := make([]DirectInboundStateUser, 0, len(emails))
	for _, email := range emails {
		rawByTag := make(map[string]json.RawMessage, len(m.InboundTags))
		for _, tag := range m.InboundTags {
			raw, ok := byTag[tag][email]
			if !ok {
				return DirectInboundState{}, fmt.Errorf("direct user %q missing from inbound %q", email, tag)
			}
			rawByTag[tag] = append(json.RawMessage(nil), raw...)
		}
		users = append(users, DirectInboundStateUser{Email: email, AdminEnabled: true, RawByTag: rawByTag})
	}
	return DirectInboundState{Version: 1, InboundTags: append([]string(nil), m.InboundTags...), Users: users}, nil
}

func (m *DirectInboundMutator) validateState(cfg map[string]any, state DirectInboundState) error {
	if state.Version != 1 {
		return fmt.Errorf("unsupported direct state version %d", state.Version)
	}
	if !sameStringSet(state.InboundTags, m.InboundTags) {
		return errors.New("direct state inbound tags do not match configured scope")
	}
	seen := map[string]struct{}{}
	for _, user := range state.Users {
		email := canonicalRelayEmail(user.Email)
		if email == "" || email != user.Email {
			return errors.New("direct state contains non-canonical email")
		}
		if _, ok := seen[email]; ok {
			return fmt.Errorf("duplicate direct state user %q", email)
		}
		seen[email] = struct{}{}
		for _, tag := range m.InboundTags {
			if len(user.RawByTag[tag]) == 0 {
				return fmt.Errorf("direct state user %q missing raw object for %q", email, tag)
			}
		}
	}
	inbounds, err := configInbounds(cfg)
	if err != nil {
		return err
	}
	for _, tag := range m.InboundTags {
		for email := range inbounds[tag] {
			if _, ok := seen[email]; !ok {
				return fmt.Errorf("direct config user %q in %q is absent from state", email, tag)
			}
		}
	}
	return nil
}

func (m *DirectInboundMutator) mutate(ctx context.Context, email string, transition func(*DirectInboundStateUser) error) error {
	if err := m.Ready(ctx); err != nil {
		return err
	}
	key := canonicalRelayEmail(email)
	if key == "" || transition == nil {
		return errors.New("direct client identity and transition are required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readConfig(ctx)
	if err != nil {
		return err
	}
	oldState, err := m.loadOrInitializeState(ctx, cfg)
	if err != nil {
		return err
	}
	nextState := cloneDirectState(oldState)
	var target *DirectInboundStateUser
	for i := range nextState.Users {
		if nextState.Users[i].Email == key {
			target = &nextState.Users[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("direct user %q not found", email)
	}
	if err := transition(target); err != nil {
		return err
	}
	candidate, err := encodeDirectCandidate(cfg, nextState)
	if err != nil {
		return err
	}
	rollback, err := m.Config.ApplyWithSingBoxCheck(ctx, candidate, m.BinaryPath)
	if err != nil {
		return err
	}
	configCommitted := false
	defer func() {
		if !configCommitted {
			if rollback.Rollback() == nil {
				_ = m.Reload(context.Background())
			}
		}
	}()
	if err := m.saveState(ctx, nextState); err != nil {
		return err
	}
	stateCommitted := false
	defer func() {
		if !stateCommitted {
			_ = m.saveState(context.Background(), oldState)
		}
	}()
	if err := m.Reload(ctx); err != nil {
		return err
	}
	if err := m.Health(ctx); err != nil {
		return err
	}
	if err := rollback.Commit(); err != nil {
		return err
	}
	configCommitted = true
	stateCommitted = true
	return nil
}

func (m *DirectInboundMutator) saveState(ctx context.Context, state DirectInboundState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filepath.Clean(m.StatePath)
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("direct state target must not be a symlink")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create direct state directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".direct-state-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return nil
}

func cloneDirectState(state DirectInboundState) DirectInboundState {
	out := state
	out.InboundTags = append([]string(nil), state.InboundTags...)
	out.Users = make([]DirectInboundStateUser, len(state.Users))
	for i, user := range state.Users {
		out.Users[i] = user
		out.Users[i].RawByTag = make(map[string]json.RawMessage, len(user.RawByTag))
		for tag, raw := range user.RawByTag {
			out.Users[i].RawByTag[tag] = append(json.RawMessage(nil), raw...)
		}
	}
	return out
}

func configInbounds(cfg map[string]any) (map[string]map[string]json.RawMessage, error) {
	raw, ok := cfg["inbounds"].([]any)
	if !ok {
		return nil, errors.New("sing-box config inbounds must be an array")
	}
	out := make(map[string]map[string]json.RawMessage, len(raw))
	for _, item := range raw {
		inbound, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("sing-box inbound must be an object")
		}
		tag, _ := inbound["tag"].(string)
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return nil, errors.New("sing-box inbound tag is required")
		}
		users, _ := inbound["users"].([]any)
		byEmail := make(map[string]json.RawMessage, len(users))
		for _, item := range users {
			obj, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("inbound %q contains non-object user", tag)
			}
			name, _ := obj["name"].(string)
			if name == "" {
				name, _ = obj["email"].(string)
			}
			name = canonicalRelayEmail(name)
			if name == "" {
				return nil, fmt.Errorf("inbound %q contains user without name/email", tag)
			}
			encoded, err := json.Marshal(obj)
			if err != nil {
				return nil, err
			}
			if _, exists := byEmail[name]; exists {
				return nil, fmt.Errorf("inbound %q contains duplicate user %q", tag, name)
			}
			byEmail[name] = encoded
		}
		out[tag] = byEmail
	}
	return out, nil
}

func encodeDirectCandidate(cfg map[string]any, state DirectInboundState) ([]byte, error) {
	data := cloneAnyMap(cfg)
	rawInbounds, ok := data["inbounds"].([]any)
	if !ok {
		return nil, errors.New("sing-box config inbounds must be an array")
	}
	targets := make(map[string]struct{}, len(state.InboundTags))
	for _, tag := range state.InboundTags {
		targets[tag] = struct{}{}
	}
	for i, item := range rawInbounds {
		inbound, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("sing-box inbound must be an object")
		}
		tag, _ := inbound["tag"].(string)
		if _, ok := targets[tag]; !ok {
			continue
		}
		users := make([]any, 0, len(state.Users))
		for _, user := range state.Users {
			if !user.AdminEnabled || user.QuotaBlocked {
				continue
			}
			raw := user.RawByTag[tag]
			var obj any
			if err := json.Unmarshal(raw, &obj); err != nil {
				return nil, fmt.Errorf("decode saved direct user %q: %w", user.Email, err)
			}
			users = append(users, obj)
		}
		inbound["users"] = users
		rawInbounds[i] = inbound
	}
	data["inbounds"] = rawInbounds
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func cloneAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func sameStringSet(a, b []string) bool {
	left := append([]string(nil), a...)
	right := append([]string(nil), b...)
	sort.Strings(left)
	sort.Strings(right)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
