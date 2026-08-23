package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service/singboxadapter"
)

func managedStateFixture() singboxadapter.ManagedRelayState {
	return singboxadapter.ManagedRelayState{
		Version: 2, InboundTag: "managed-relay-hk1", ListenPort: 29001,
		CertificatePath: "/etc/sing-box/cert.pem", KeyPath: "/etc/sing-box/key.pem",
		Exits: []singboxadapter.ManagedRelayExit{{Tag: "us2", Server: "us2.example", ServerPort: 443, Password: "exit-secret"}},
		Users: []singboxadapter.ManagedRelayStateUser{{
			ManagedRelayUser: singboxadapter.ManagedRelayUser{Email: "alice@example.com", Password: "alice-secret", ExitTag: "us2"},
			Enabled:          true, AdminEnabled: true,
		}},
	}
}

func TestManagedMutatorFromEnvironmentIsDisabledByDefault(t *testing.T) {
	t.Setenv("SINGBOX_ADAPTER_MANAGED", "false")
	mutator, err := managedMutatorFromEnvironment("/does/not/exist")
	if err != nil || mutator != nil {
		t.Fatalf("mutator=%v err=%v", mutator, err)
	}
}

func TestManagedMutatorFromEnvironmentFailsClosedWithoutState(t *testing.T) {
	t.Setenv("SINGBOX_ADAPTER_MANAGED", "true")
	t.Setenv("SINGBOX_ADAPTER_MANAGED_STATE", filepath.Join(t.TempDir(), "missing-state.json"))
	t.Setenv("SINGBOX_ADAPTER_SINGBOX_BIN", "/bin/true")
	if _, err := managedMutatorFromEnvironment(filepath.Join(t.TempDir(), "config.json")); err == nil {
		t.Fatal("managed mode accepted missing canonical state")
	}
}

func TestManagedMutatorFromEnvironmentConstructsReadyMutator(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	configPath := filepath.Join(dir, "config.json")
	stateBytes, err := json.Marshal(managedStateFixture())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, stateBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"inbounds":[],"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(dir, "sing-box")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SINGBOX_ADAPTER_MANAGED", "true")
	t.Setenv("SINGBOX_ADAPTER_MANAGED_STATE", statePath)
	t.Setenv("SINGBOX_ADAPTER_SINGBOX_BIN", fakeBin)
	t.Setenv("SINGBOX_ADAPTER_SINGBOX_SERVICE", "sing-box-test.service")
	mutator, err := managedMutatorFromEnvironment(configPath)
	if err != nil {
		t.Fatalf("managed mutator: %v", err)
	}
	if mutator == nil || mutator.State == nil || mutator.Config == nil {
		t.Fatalf("mutator=%+v", mutator)
	}
	if err := mutator.Ready(context.Background()); err != nil {
		t.Fatalf("Ready: %v", err)
	}
}
