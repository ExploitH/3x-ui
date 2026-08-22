package singboxadapter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func testManagedRelayState() ManagedRelayState {
	return ManagedRelayState{
		Version: 1, InboundTag: "managed-relay-hk1", ListenPort: 29001, CertificatePath: "/etc/sing-box/cert.pem", KeyPath: "/etc/sing-box/key.pem",
		Exits: []ManagedRelayExit{{Tag: "us2", Server: "us2.example", ServerPort: 443, Password: "exit-secret"}},
		Users: []ManagedRelayStateUser{{
			ManagedRelayUser: ManagedRelayUser{Email: "alice@example.com", Password: "alice-uuid", ExitTag: "us2"},
			Enabled:          true,
		}},
	}
}

func TestManagedRelayStateStoreSaveLoadAndUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managed-state.json")
	store := NewManagedRelayStateStore(path)
	if err := store.Save(context.Background(), testManagedRelayState()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode=%o, want 600", info.Mode().Perm())
	}
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Users[0].Password != "alice-uuid" || !loaded.Users[0].Enabled {
		t.Fatalf("loaded state=%+v", loaded)
	}
	if err := store.Update(context.Background(), func(state ManagedRelayState) (ManagedRelayState, error) {
		return state.SetUserEnabled("alice@example.com", false)
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Users[0].Enabled {
		t.Fatal("state update did not persist disabled flag")
	}
}

func TestManagedRelayStateStoreRejectsInvalidOrSymlinkState(t *testing.T) {
	dir := t.TempDir()
	invalid := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(invalid, []byte(`{"version":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManagedRelayStateStore(invalid).Load(context.Background()); err == nil {
		t.Fatal("invalid state accepted")
	}
	realPath := filepath.Join(dir, "real.json")
	if err := os.WriteFile(realPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(dir, "link.json")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatal(err)
	}
	if err := NewManagedRelayStateStore(linkPath).Save(context.Background(), testManagedRelayState()); err == nil {
		t.Fatal("symlink state accepted")
	}
}

func TestManagedRelayStateStoreNeverSerializesMalformedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewManagedRelayStateStore(path)
	state := testManagedRelayState()
	state.Users[0].Password = ""
	if err := store.Save(context.Background(), state); err == nil {
		t.Fatal("malformed state accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("malformed save created state: %v", err)
	}
	// Ensure the valid JSON shape remains stable for future migration readers.
	encoded, err := json.Marshal(testManagedRelayState())
	if err != nil || len(encoded) == 0 {
		t.Fatalf("marshal state err=%v", err)
	}
}
