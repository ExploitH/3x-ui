package singboxadapter

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEncodeManagedConfigMergesOnlyManagedEntries(t *testing.T) {
	legacy := map[string]any{
		"log":       map[string]any{"level": "info"},
		"inbounds":  []any{map[string]any{"type": "vless", "tag": "legacy-in"}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
	}
	fragment, err := BuildManagedRelayFragment(RelayFragmentInput{
		InboundTag: "managed-relay-hk1", ListenPort: 29001,
		Users: []ManagedRelayUser{{Email: "alice@example.com", UUID: "alice-uuid", ExitTag: "us2"}},
		Exits: []ManagedRelayExit{{Tag: "us2", Server: "us2.example", ServerPort: 443}},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeManagedConfig(legacy, fragment)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["log"].(map[string]any)["level"] != "info" {
		t.Fatalf("legacy log was changed: %#v", decoded["log"])
	}
	if got := decoded["inbounds"].([]any)[1].(map[string]any)["tag"]; got != "managed-relay-hk1" {
		t.Fatalf("managed inbound=%v", got)
	}
}

func TestManagedConfigStoreValidatorFailureLeavesOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	original := []byte(`{"log":{"level":"info"}}\n`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewManagedConfigStore(path)
	_, err := store.Apply(context.Background(), []byte(`{"invalid":true}\n`), func(context.Context, string) error {
		return errors.New("sing-box check failed")
	})
	if err == nil {
		t.Fatal("validator failure was accepted")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(original) {
		t.Fatalf("original changed after validation failure: %q", got)
	}
}

func TestManagedConfigStoreApplyPreservesModeAndRollback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	original := []byte(`{"log":{"level":"info"}}\n`)
	candidate := []byte(`{"log":{"level":"warn"}}\n`)
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatal(err)
	}
	store := NewManagedConfigStore(path)
	handle, err := store.Apply(context.Background(), candidate, nil)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode=%o, want 640", info.Mode().Perm())
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(candidate) {
		t.Fatalf("candidate=%q err=%v", got, err)
	}
	if err := handle.Rollback(); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(path)
	if err != nil || string(got) != string(original) {
		t.Fatalf("rollback=%q err=%v", got, err)
	}
}

func TestManagedConfigStoreRejectsSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	realPath := filepath.Join(dir, "real.json")
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(realPath, []byte(`{"ok":true}\n`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realPath, path); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManagedConfigStore(path).Apply(context.Background(), []byte(`{"ok":false}\n`), nil); err == nil {
		t.Fatal("symlink target accepted")
	}
	got, err := os.ReadFile(realPath)
	if err != nil || string(got) != `{"ok":true}\n` {
		t.Fatalf("real target=%q err=%v", got, err)
	}
}
