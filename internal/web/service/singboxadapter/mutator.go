package singboxadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

type ManagedRelayMutator struct {
	mu         sync.Mutex
	State      *ManagedRelayStateStore
	Config     *ManagedConfigStore
	BinaryPath string
	Reload     func(context.Context) error
	Health     func(context.Context) error
}

func (m *ManagedRelayMutator) Ready(ctx context.Context) error {
	if m == nil || m.State == nil || m.Config == nil {
		return errors.New("managed relay mutator stores are required")
	}
	if m.BinaryPath == "" || m.Reload == nil || m.Health == nil {
		return errors.New("managed relay mutator validation and callbacks are required")
	}
	state, err := m.State.Load(ctx)
	if err != nil {
		return err
	}
	if len(state.Exits) == 0 {
		return errors.New("managed relay has no configured exits")
	}
	return nil
}

func (m *ManagedRelayMutator) SetUserEnabled(ctx context.Context, email string, enabled bool) error {
	return m.mutate(ctx, func(state ManagedRelayState) (ManagedRelayState, error) {
		return state.SetUserAdminEnabled(email, enabled)
	})
}

func (m *ManagedRelayMutator) SetUserQuotaBlocked(ctx context.Context, email string, blocked bool) error {
	return m.mutate(ctx, func(state ManagedRelayState) (ManagedRelayState, error) {
		return state.SetUserQuotaBlocked(email, blocked)
	})
}

func (m *ManagedRelayMutator) UpsertUser(ctx context.Context, user ManagedRelayUser, enabled bool) error {
	return m.mutate(ctx, func(state ManagedRelayState) (ManagedRelayState, error) {
		return state.UpsertUser(user, enabled)
	})
}

func (m *ManagedRelayMutator) AddClient(ctx context.Context, payload ManagedRelayClient) error {
	return m.mutate(ctx, func(state ManagedRelayState) (ManagedRelayState, error) {
		user, err := payload.ToUser(state)
		if err != nil {
			return ManagedRelayState{}, err
		}
		return state.UpsertUser(user, payload.Enable)
	})
}

func (m *ManagedRelayMutator) UpdateClient(ctx context.Context, oldEmail string, payload ManagedRelayClient) error {
	return m.mutate(ctx, func(state ManagedRelayState) (ManagedRelayState, error) {
		if strings.TrimSpace(payload.Email) == "" {
			payload.Email = oldEmail
		}
		user, err := payload.ToUser(state)
		if err != nil {
			return ManagedRelayState{}, err
		}
		oldKey := canonicalRelayEmail(oldEmail)
		newKey := canonicalRelayEmail(user.Email)
		if oldKey == "" || oldKey == newKey {
			return state.UpsertUser(user, payload.Enable)
		}
		withoutOld, err := state.RemoveUser(oldKey)
		if err != nil {
			return ManagedRelayState{}, err
		}
		return withoutOld.UpsertUser(user, payload.Enable)
	})
}

func (m *ManagedRelayMutator) DetachClient(ctx context.Context, email string) error {
	return m.mutate(ctx, func(state ManagedRelayState) (ManagedRelayState, error) {
		return state.DetachUser(email)
	})
}

func (m *ManagedRelayMutator) DeleteClient(ctx context.Context, email string) error {
	return m.mutate(ctx, func(state ManagedRelayState) (ManagedRelayState, error) {
		return state.RemoveUser(email)
	})
}

func (m *ManagedRelayMutator) UpdateUser(ctx context.Context, oldEmail string, user ManagedRelayUser, enabled bool) error {
	return m.mutate(ctx, func(state ManagedRelayState) (ManagedRelayState, error) {
		oldKey := canonicalRelayEmail(oldEmail)
		newKey := canonicalRelayEmail(user.Email)
		if oldKey == "" || oldKey == newKey {
			return state.UpsertUser(user, enabled)
		}
		withoutOld, err := state.RemoveUser(oldKey)
		if err != nil {
			return ManagedRelayState{}, err
		}
		return withoutOld.UpsertUser(user, enabled)
	})
}

func (m *ManagedRelayMutator) RemoveUser(ctx context.Context, email string) error {
	return m.mutate(ctx, func(state ManagedRelayState) (ManagedRelayState, error) {
		return state.RemoveUser(email)
	})
}

func (m *ManagedRelayMutator) mutate(ctx context.Context, transition func(ManagedRelayState) (ManagedRelayState, error)) error {
	if err := m.Ready(ctx); err != nil {
		return err
	}
	if transition == nil {
		return errors.New("managed relay state transition is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	oldState, err := m.State.Load(ctx)
	if err != nil {
		return err
	}
	oldConfig, err := m.Config.Read(ctx)
	if err != nil {
		return err
	}
	nextState, err := transition(oldState)
	if err != nil {
		return err
	}
	var legacy map[string]any
	if err := json.Unmarshal(oldConfig, &legacy); err != nil {
		return fmt.Errorf("decode current managed config: %w", err)
	}
	fragment, err := nextState.ActiveFragment()
	if err != nil {
		return err
	}
	candidate, err := EncodeManagedConfig(legacy, fragment)
	if err != nil {
		return err
	}

	configRollback, err := m.Config.ApplyWithSingBoxCheck(ctx, candidate, m.BinaryPath)
	if err != nil {
		return err
	}
	rollbackConfig := true
	defer func() {
		if rollbackConfig {
			if err := configRollback.Rollback(); err == nil {
				_ = m.Reload(context.Background())
			}
		}
	}()

	if err := m.State.Save(ctx, nextState); err != nil {
		return fmt.Errorf("save managed relay state: %w", err)
	}
	rollbackState := true
	defer func() {
		if rollbackState {
			_ = m.State.Save(context.Background(), oldState)
		}
	}()

	if err := m.Reload(ctx); err != nil {
		return fmt.Errorf("reload managed relay: %w", err)
	}
	if err := m.Health(ctx); err != nil {
		return fmt.Errorf("managed relay health check: %w", err)
	}
	if err := configRollback.Commit(); err != nil {
		return fmt.Errorf("commit managed relay config: %w", err)
	}
	rollbackConfig = false
	rollbackState = false
	return nil
}
