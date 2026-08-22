package singboxadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func (m *ManagedRelayMutator) SetUserEnabled(ctx context.Context, email string, enabled bool) error {
	if m == nil || m.State == nil || m.Config == nil {
		return errors.New("managed relay mutator stores are required")
	}
	if m.BinaryPath == "" || m.Reload == nil || m.Health == nil {
		return errors.New("managed relay mutator validation and callbacks are required")
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
	nextState, err := oldState.SetUserEnabled(email, enabled)
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
			_ = configRollback.Rollback()
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
