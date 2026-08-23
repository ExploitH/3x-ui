package singboxadapter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func directTestConfig(t *testing.T) string {
	t.Helper()
	cfg := map[string]any{
		"inbounds": []any{
			map[string]any{"tag": "direct-vless", "type": "vless", "listen_port": 8881, "users": []any{
				map[string]any{"name": "alice@example.com", "uuid": "alice-uuid", "flow": "xtls-rprx-vision"},
			}},
			map[string]any{"tag": "direct-hy2", "type": "hysteria2", "listen_port": 8882, "users": []any{
				map[string]any{"name": "alice@example.com", "password": "alice-password"},
			}},
			map[string]any{"tag": "direct-tuic", "type": "tuic", "listen_port": 8883, "users": []any{
				map[string]any{"name": "alice@example.com", "uuid": "alice-tuic", "password": "alice-tuic-password"},
			}},
			map[string]any{"tag": "unmanaged", "type": "hysteria2", "listen_port": 443, "users": []any{
				map[string]any{"password": "legacy-password"},
			}},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func directTestMutator(t *testing.T) *DirectInboundMutator {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "sing-box")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return &DirectInboundMutator{
		StatePath: filepath.Join(dir, "managed", "direct-state.json"),
		Config:    NewManagedConfigStore(directTestConfig(t)), BinaryPath: binary,
		InboundTags: []string{"direct-vless", "direct-hy2", "direct-tuic"},
		Reload:      func(context.Context) error { return nil }, Health: func(context.Context) error { return nil },
	}
}

func directConfigUsers(t *testing.T, path string) map[string]int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, raw := range cfg["inbounds"].([]any) {
		ib := raw.(map[string]any)
		out[ib["tag"].(string)] = len(ib["users"].([]any))
	}
	return out
}

func TestDirectInboundMutatorQuotaBlockAndResetRestoresExactUsers(t *testing.T) {
	m := directTestMutator(t)
	if err := m.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.SetUserQuotaBlocked(context.Background(), "alice@example.com", true); err != nil {
		t.Fatal(err)
	}
	got := directConfigUsers(t, m.Config.path)
	for _, tag := range m.InboundTags {
		if got[tag] != 0 {
			t.Fatalf("blocked %s users=%d, want 0", tag, got[tag])
		}
	}
	if err := m.SetUserQuotaBlocked(context.Background(), "alice@example.com", false); err != nil {
		t.Fatal(err)
	}
	got = directConfigUsers(t, m.Config.path)
	for _, tag := range m.InboundTags {
		if got[tag] != 1 {
			t.Fatalf("restored %s users=%d, want 1", tag, got[tag])
		}
	}
}

func TestDirectInboundMutatorAdminDisableIsNotOverriddenByQuotaReset(t *testing.T) {
	m := directTestMutator(t)
	if err := m.SetUserAdminEnabled(context.Background(), "alice@example.com", false); err != nil {
		t.Fatal(err)
	}
	if err := m.SetUserQuotaBlocked(context.Background(), "alice@example.com", true); err != nil {
		t.Fatal(err)
	}
	if err := m.SetUserQuotaBlocked(context.Background(), "alice@example.com", false); err != nil {
		t.Fatal(err)
	}
	got := directConfigUsers(t, m.Config.path)
	for _, tag := range m.InboundTags {
		if got[tag] != 0 {
			t.Fatalf("admin-disabled %s users=%d, want 0", tag, got[tag])
		}
	}
}

func TestDirectInboundMutatorFailsClosedOnUnknownCurrentUser(t *testing.T) {
	m := directTestMutator(t)
	if err := m.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(m.Config.path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["inbounds"].([]any)[0].(map[string]any)["users"] = append(cfg["inbounds"].([]any)[0].(map[string]any)["users"].([]any), map[string]any{"name": "unknown@example.com", "uuid": "u"})
	data, _ = json.Marshal(cfg)
	if err := os.WriteFile(m.Config.path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Ready(context.Background()); err == nil {
		t.Fatal("Ready accepted a current user absent from canonical state")
	}
}
