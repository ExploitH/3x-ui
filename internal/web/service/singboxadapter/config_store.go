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

// EncodeManagedConfig merges a managed fragment into an existing decoded
// sing-box config. Only managed-* entries are replaced; legacy entries remain
// untouched. The result is deterministic JSON suitable for atomic installation.
func EncodeManagedConfig(legacy map[string]any, fragment RelayFragment) ([]byte, error) {
	merged, err := MergeManagedRelayFragment(legacy, fragment)
	if err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode managed sing-box config: %w", err)
	}
	return append(encoded, '\n'), nil
}

// ManagedConfigStore installs a candidate config with same-directory temp-file
// + fsync + atomic rename semantics. It is deliberately independent from
// sing-box process control; callers must validate/reload separately.
type ManagedConfigStore struct {
	path string
}

func NewManagedConfigStore(path string) *ManagedConfigStore {
	return &ManagedConfigStore{path: filepath.Clean(strings.TrimSpace(path))}
}

// ApplyWithSingBoxCheck is the production-facing convenience path: a candidate
// cannot be installed unless the exact configured sing-box binary accepts it.
func (s *ManagedConfigStore) ApplyWithSingBoxCheck(ctx context.Context, candidate []byte, binaryPath string) (*ManagedConfigRollback, error) {
	return s.Apply(ctx, candidate, func(validateCtx context.Context, candidatePath string) error {
		return ValidateSingBoxConfig(validateCtx, binaryPath, candidatePath)
	})
}

type ManagedConfigRollback struct {
	mu         sync.Mutex
	path       string
	backupPath string
	dir        string
	done       bool
}

func (s *ManagedConfigStore) Apply(ctx context.Context, candidate []byte, validator func(context.Context, string) error) (*ManagedConfigRollback, error) {
	if s == nil || s.path == "" || s.path == "." {
		return nil, errors.New("managed config path is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Lstat(s.path)
	if err != nil {
		return nil, fmt.Errorf("stat managed config: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("managed config target must be a regular non-symlink file")
	}

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".singbox-config-candidate-*")
	if err != nil {
		return nil, fmt.Errorf("create managed config candidate: %w", err)
	}
	tmpPath := tmp.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("preserve managed config mode: %w", err)
	}
	if _, err := tmp.Write(candidate); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("write managed config candidate: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("sync managed config candidate: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close managed config candidate: %w", err)
	}

	if validator != nil {
		if err := validator(ctx, tmpPath); err != nil {
			return nil, fmt.Errorf("validate managed config candidate: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	backup, err := os.CreateTemp(dir, ".singbox-config-backup-*")
	if err != nil {
		return nil, fmt.Errorf("create managed config backup: %w", err)
	}
	backupPath := backup.Name()
	removeBackup := true
	defer func() {
		if removeBackup {
			_ = os.Remove(backupPath)
		}
	}()
	if err := backup.Chmod(info.Mode().Perm()); err != nil {
		_ = backup.Close()
		return nil, fmt.Errorf("preserve managed config backup mode: %w", err)
	}
	original, err := os.Open(s.path)
	if err != nil {
		_ = backup.Close()
		return nil, fmt.Errorf("open managed config for backup: %w", err)
	}
	if _, err := io.Copy(backup, original); err != nil {
		_ = original.Close()
		_ = backup.Close()
		return nil, fmt.Errorf("copy managed config backup: %w", err)
	}
	if err := original.Close(); err != nil {
		_ = backup.Close()
		return nil, fmt.Errorf("close managed config source: %w", err)
	}
	if err := backup.Sync(); err != nil {
		_ = backup.Close()
		return nil, fmt.Errorf("sync managed config backup: %w", err)
	}
	if err := backup.Close(); err != nil {
		return nil, fmt.Errorf("close managed config backup: %w", err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		return nil, fmt.Errorf("install managed config atomically: %w", err)
	}
	cleanupTemp = false
	if err := syncDirectory(dir); err != nil {
		// The rename already happened; retain the backup and return a rollback
		// handle so the caller can recover instead of claiming durable install.
		removeBackup = false
		return &ManagedConfigRollback{path: s.path, backupPath: backupPath, dir: dir}, fmt.Errorf("sync managed config directory: %w", err)
	}
	removeBackup = false
	return &ManagedConfigRollback{path: s.path, backupPath: backupPath, dir: dir}, nil
}

func (h *ManagedConfigRollback) Rollback() error {
	if h == nil {
		return errors.New("managed config rollback handle is nil")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.done {
		return nil
	}
	if err := ensureRegularNonSymlink(h.path); err != nil {
		return err
	}
	if err := os.Rename(h.backupPath, h.path); err != nil {
		return fmt.Errorf("rollback managed config: %w", err)
	}
	if err := syncDirectory(h.dir); err != nil {
		return fmt.Errorf("sync managed config rollback: %w", err)
	}
	h.done = true
	return nil
}

// Commit discards the durable rollback copy after the caller has completed its
// post-install health check. Until Commit or Rollback, the backup is retained.
func (h *ManagedConfigRollback) Commit() error {
	if h == nil {
		return errors.New("managed config rollback handle is nil")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.done {
		return nil
	}
	if err := os.Remove(h.backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove managed config backup: %w", err)
	}
	h.done = true
	return syncDirectory(h.dir)
}

func ensureRegularNonSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat managed config: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("managed config target must be a regular non-symlink file")
	}
	return nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
