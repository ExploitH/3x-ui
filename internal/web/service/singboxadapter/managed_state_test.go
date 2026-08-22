package singboxadapter

import "testing"

func TestManagedRelayStateKeepsDisabledCanonicalSpecAndRebuildsExit(t *testing.T) {
	state := ManagedRelayState{
		Version: 1, InboundTag: "managed-relay-hk1", ListenPort: 29001,
		Exits: []ManagedRelayExit{{Tag: "us2", Server: "us2.example", ServerPort: 443}},
		Users: []ManagedRelayStateUser{
			{ManagedRelayUser: ManagedRelayUser{Email: "alice@example.com", UUID: "alice-uuid", ExitTag: "us2"}, Enabled: true},
			{ManagedRelayUser: ManagedRelayUser{Email: "bob@example.com", UUID: "bob-uuid", ExitTag: "us2"}, Enabled: false},
		},
	}
	fragment, err := state.ActiveFragment()
	if err != nil {
		t.Fatal(err)
	}
	if len(fragment.Inbounds) != 1 || len(fragment.Inbounds[0].Users) != 1 || fragment.Inbounds[0].Users[0].Email != "alice@example.com" {
		t.Fatalf("active users=%+v", fragment.Inbounds)
	}
	updated, err := state.SetUserEnabled("alice@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	fragment, err = updated.ActiveFragment()
	if err != nil {
		t.Fatal(err)
	}
	if len(fragment.Inbounds[0].Users) != 0 || len(fragment.Outbounds) != 1 {
		t.Fatalf("all-disabled fragment=%+v", fragment)
	}
	updated, err = updated.SetUserEnabled("alice@example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	fragment, err = updated.ActiveFragment()
	if err != nil {
		t.Fatal(err)
	}
	if len(fragment.Inbounds[0].Users) != 1 || fragment.Inbounds[0].Users[0].UUID != "alice-uuid" || len(fragment.Outbounds) != 2 {
		t.Fatalf("restored fragment=%+v", fragment)
	}
	var bob ManagedRelayStateUser
	for _, user := range updated.Users {
		if user.Email == "bob@example.com" {
			bob = user
		}
	}
	if bob.UUID != "bob-uuid" || bob.ExitTag != "us2" || bob.Enabled {
		t.Fatalf("disabled canonical state lost=%+v", bob)
	}
}

func TestManagedRelayStateRejectsUnknownAndDuplicateUsers(t *testing.T) {
	state := ManagedRelayState{
		Version: 1, InboundTag: "managed-relay-hk1", ListenPort: 29001,
		Exits: []ManagedRelayExit{{Tag: "us2", Server: "us2.example", ServerPort: 443}},
		Users: []ManagedRelayStateUser{
			{ManagedRelayUser: ManagedRelayUser{Email: "alice@example.com", UUID: "alice-uuid", ExitTag: "us2"}, Enabled: true},
			{ManagedRelayUser: ManagedRelayUser{Email: "alice@example.com", UUID: "other-uuid", ExitTag: "us2"}, Enabled: false},
		},
	}
	if _, err := state.ActiveFragment(); err == nil {
		t.Fatal("duplicate canonical email accepted")
	}
	state.Users = state.Users[:1]
	if _, err := state.SetUserEnabled("missing@example.com", true); err == nil {
		t.Fatal("unknown user enable accepted")
	}
}

func TestManagedRelayStateUpsertAndRemoveAreCanonical(t *testing.T) {
	state := ManagedRelayState{Version: 1, InboundTag: "managed-relay-hk1", ListenPort: 29001, Exits: []ManagedRelayExit{{Tag: "us2", Server: "us2.example", ServerPort: 443}}}
	var err error
	state, err = state.UpsertUser(ManagedRelayUser{Email: " Alice@example.com ", UUID: "alice-uuid", ExitTag: "us2"}, false)
	if err != nil {
		t.Fatal(err)
	}
	state, err = state.SetUserEnabled("alice@example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	fragment, err := state.ActiveFragment()
	if err != nil || len(fragment.Inbounds[0].Users) != 1 || fragment.Inbounds[0].Users[0].Email != "alice@example.com" {
		t.Fatalf("canonical upsert fragment=%+v err=%v", fragment, err)
	}
	state, err = state.RemoveUser(" ALICE@EXAMPLE.COM ")
	if err != nil || len(state.Users) != 0 {
		t.Fatalf("canonical remove state=%+v err=%v", state, err)
	}
}
