package singboxadapter

import "testing"

func TestManagedRelayStateSeparatesAdminDisableFromQuotaBlock(t *testing.T) {
	state := ManagedRelayState{
		Version: 2, InboundTag: "managed-relay-hk1", ListenPort: 29001,
		CertificatePath: "/etc/sing-box/cert.pem", KeyPath: "/etc/sing-box/key.pem",
		Exits: []ManagedRelayExit{{Tag: "us2", Server: "us2.example", ServerPort: 443, Password: "exit-secret"}},
		Users: []ManagedRelayStateUser{{
			ManagedRelayUser: ManagedRelayUser{Email: "alice@example.com", Password: "alice-secret", ExitTag: "us2"},
			Enabled:          true, AdminEnabled: true,
		}},
	}
	var err error
	state, err = state.SetUserAdminEnabled("alice@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if state.Users[0].Enabled || state.Users[0].AdminEnabled || state.Users[0].QuotaBlocked {
		t.Fatalf("admin disable state=%+v", state.Users[0])
	}
	state, err = state.SetUserQuotaBlocked("alice@example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	state, err = state.SetUserQuotaBlocked("alice@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if state.Users[0].Enabled || state.Users[0].AdminEnabled || state.Users[0].QuotaBlocked {
		t.Fatalf("quota reset incorrectly restored admin-disabled user=%+v", state.Users[0])
	}
	state, err = state.SetUserAdminEnabled("alice@example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Users[0].Enabled || !state.Users[0].AdminEnabled || state.Users[0].QuotaBlocked {
		t.Fatalf("admin re-enable state=%+v", state.Users[0])
	}
}

func TestManagedRelayStateQuotaBlockResetRestoresOnlyQuotaBlockedUser(t *testing.T) {
	state := ManagedRelayState{
		Version: 2, InboundTag: "managed-relay-hk1", ListenPort: 29001,
		CertificatePath: "/etc/sing-box/cert.pem", KeyPath: "/etc/sing-box/key.pem",
		Exits: []ManagedRelayExit{{Tag: "us2", Server: "us2.example", ServerPort: 443, Password: "exit-secret"}},
		Users: []ManagedRelayStateUser{{
			ManagedRelayUser: ManagedRelayUser{Email: "alice@example.com", Password: "alice-secret", ExitTag: "us2"},
			Enabled:          true, AdminEnabled: true,
		}},
	}
	var err error
	state, err = state.SetUserQuotaBlocked("alice@example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	if state.Users[0].Enabled || !state.Users[0].AdminEnabled || !state.Users[0].QuotaBlocked {
		t.Fatalf("quota block state=%+v", state.Users[0])
	}
	state, err = state.SetUserQuotaBlocked("alice@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Users[0].Enabled || !state.Users[0].AdminEnabled || state.Users[0].QuotaBlocked {
		t.Fatalf("quota reset state=%+v", state.Users[0])
	}
}
