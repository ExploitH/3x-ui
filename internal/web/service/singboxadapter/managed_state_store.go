package singboxadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const maxManagedRelayStateBytes = 4 << 20

type ManagedRelayStateStore struct {
	mu   sync.Mutex
	path string
}

func NewManagedRelayStateStore(path string) *ManagedRelayStateStore {
	return &ManagedRelayStateStore{path: filepath.Clean(strings.TrimSpace(path))}
}

func (s *ManagedRelayStateStore) Load(ctx context.Context) (ManagedRelayState, error) {
	if s == nil || s.path == "" || s.path == "." {
		return ManagedRelayState{}, errors.New("managed relay state path is required")
	}
	if err := ctx.Err(); err != nil {
		return ManagedRelayState{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadUnlocked(ctx)
}

func (s *ManagedRelayStateStore) Save(ctx context.Context, state ManagedRelayState) error {
	if s == nil || s.path == "" || s.path == "." {
		return errors.New("managed relay state path is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := state.validate(); err != nil {
		return fmt.Errorf("validate managed relay state: %w", err)
	}
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode managed relay state: %w", err)
	}
	encoded = append(encoded, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveBytesUnlocked(ctx, encoded)
}

func (s *ManagedRelayStateStore) Update(ctx context.Context, mutate func(ManagedRelayState) (ManagedRelayState, error)) error {
	if s == nil || s.path == "" || s.path == "." {
		return errors.New("managed relay state path is required")
	}
	if mutate == nil {
		return errors.New("managed relay state mutation is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadUnlocked(ctx)
	if err != nil {
		return err
	}
	next, err := mutate(state)
	if err != nil {
		return err
	}
	if err := next.validate(); err != nil {
		return fmt.Errorf("validate managed relay state update: %w", err)
	}
	encoded, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf("encode managed relay state update: %w", err)
	}
	return s.saveBytesUnlocked(ctx, append(encoded, '\n'))
}

func (s *ManagedRelayStateStore) loadUnlocked(ctx context.Context) (ManagedRelayState, error) {
	if err := ctx.Err(); err != nil {
		return ManagedRelayState{}, err
	}
	info, err := os.Lstat(s.path)
	if err != nil {
		return ManagedRelayState{}, fmt.Errorf("stat managed relay state: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return ManagedRelayState{}, errors.New("managed relay state must be a regular non-symlink file")
	}
	file, err := os.Open(s.path)
	if err != nil {
		return ManagedRelayState{}, fmt.Errorf("open managed relay state: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxManagedRelayStateBytes+1))
	closeErr := file.Close()
	if readErr != nil {
		return ManagedRelayState{}, fmt.Errorf("read managed relay state: %w", readErr)
	}
	if closeErr != nil {
		return ManagedRelayState{}, fmt.Errorf("close managed relay state: %w", closeErr)
	}
	if len(data) > maxManagedRelayStateBytes {
		return ManagedRelayState{}, errors.New("managed relay state exceeds size limit")
	}
	var state ManagedRelayState
	if err := json.Unmarshal(data, &state); err != nil {
		return ManagedRelayState{}, fmt.Errorf("decode managed relay state: %w", err)
	}
	if err := state.validate(); err != nil {
		return ManagedRelayState{}, fmt.Errorf("validate managed relay state: %w", err)
	}
	return state, nil
}

func (s *ManagedRelayStateStore) saveBytesUnlocked(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create managed relay state directory: %w", err)
	}
	if info, err := os.Lstat(s.path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("managed relay state must be a regular non-symlink file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat managed relay state target: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".managed-relay-state-*")
	if err != nil {
		return fmt.Errorf("create managed relay state temp: %w", err)
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod managed relay state temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write managed relay state temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync managed relay state temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close managed relay state temp: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("install managed relay state: %w", err)
	}
	removeTemp = false
	if err := syncDirectory(dir); err != nil {
		return fmt.Errorf("sync managed relay state directory: %w", err)
	}
	return nil
}
