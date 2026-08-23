package singboxadapter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMutatorFixture(t *testing.T) (*ManagedRelayMutator, string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	statePath := filepath.Join(dir, "managed-state.json")
	binaryPath := filepath.Join(dir, "sing-box-check")
	legacy := map[string]any{
		"log":       map[string]any{"level": "error"},
		"inbounds":  []any{},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		"route":     map[string]any{"final": "direct"},
	}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	state := ManagedRelayState{
		Version: 1, InboundTag: "managed-relay-hk1", ListenPort: 29001,
		CertificatePath: "/etc/sing-box/cert.pem", KeyPath: "/etc/sing-box/key.pem",
		Exits: []ManagedRelayExit{{Tag: "us2", Server: "us2.example", ServerPort: 443, Password: "exit-secret"}},
		Users: []ManagedRelayStateUser{{ManagedRelayUser: ManagedRelayUser{Email: "alice@example.com", Password: "alice-secret", ExitTag: "us2"}, Enabled: true}},
	}
	stateStore := NewManagedRelayStateStore(statePath)
	if err := stateStore.Save(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	mutator := &ManagedRelayMutator{
		State: stateStore, Config: NewManagedConfigStore(configPath), BinaryPath: binaryPath,
		Reload: func(context.Context) error { return nil },
		Health: func(context.Context) error { return nil },
	}
	return mutator, configPath, statePath
}

func TestManagedRelayMutatorDisableAndEnableAreTransactional(t *testing.T) {
	mutator, configPath, statePath := writeMutatorFixture(t)
	if err := mutator.SetUserEnabled(context.Background(), "alice@example.com", false); err != nil {
		t.Fatal(err)
	}
	state, err := NewManagedRelayStateStore(statePath).Load(context.Background())
	if err != nil || state.Users[0].Enabled {
		t.Fatalf("disabled state=%+v err=%v", state, err)
	}
	config, err := os.ReadFile(configPath)
	if err != nil || strings.Contains(string(config), "alice@example.com") {
		t.Fatalf("disabled config=%q err=%v", config, err)
	}
	if err := mutator.SetUserEnabled(context.Background(), "alice@example.com", true); err != nil {
		t.Fatal(err)
	}
	state, err = NewManagedRelayStateStore(statePath).Load(context.Background())
	if err != nil || !state.Users[0].Enabled {
		t.Fatalf("enabled state=%+v err=%v", state, err)
	}
	config, err = os.ReadFile(configPath)
	if err != nil || !strings.Contains(string(config), "alice@example.com") || !strings.Contains(string(config), "alice-secret") {
		t.Fatalf("enabled config=%q err=%v", config, err)
	}
}

func TestManagedRelayMutatorReloadFailureRollsBackBothFiles(t *testing.T) {
	mutator, configPath, statePath := writeMutatorFixture(t)
	originalConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	mutator.Reload = func(context.Context) error { return os.ErrPermission }
	if err := mutator.SetUserEnabled(context.Background(), "alice@example.com", false); err == nil {
		t.Fatal("reload failure accepted")
	}
	config, err := os.ReadFile(configPath)
	if err != nil || string(config) != string(originalConfig) {
		t.Fatalf("config rollback=%q err=%v", config, err)
	}
	state, err := NewManagedRelayStateStore(statePath).Load(context.Background())
	if err != nil || !state.Users[0].Enabled {
		t.Fatalf("state rollback=%+v err=%v", state, err)
	}
}

func TestManagedRelayMutatorHealthFailureReloadsRestoredConfig(t *testing.T) {
	mutator, configPath, statePath := writeMutatorFixture(t)
	originalConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	reloadCalls := 0
	mutator.Reload = func(context.Context) error {
		reloadCalls++
		return nil
	}
	mutator.Health = func(context.Context) error { return os.ErrInvalid }
	if err := mutator.SetUserEnabled(context.Background(), "alice@example.com", false); err == nil {
		t.Fatal("health failure accepted")
	}
	if reloadCalls != 2 {
		t.Fatalf("reload calls=%d, want apply plus rollback reload", reloadCalls)
	}
	config, err := os.ReadFile(configPath)
	if err != nil || string(config) != string(originalConfig) {
		t.Fatalf("config rollback=%q err=%v", config, err)
	}
	state, err := NewManagedRelayStateStore(statePath).Load(context.Background())
	if err != nil || !state.Users[0].Enabled {
		t.Fatalf("state rollback=%+v err=%v", state, err)
	}
}

func TestManagedRelayMutatorRequiresReloadAndHealthCallbacks(t *testing.T) {
	mutator, _, _ := writeMutatorFixture(t)
	mutator.Reload = nil
	if err := mutator.SetUserEnabled(context.Background(), "alice@example.com", false); err == nil {
		t.Fatal("nil reload callback accepted")
	}
}
