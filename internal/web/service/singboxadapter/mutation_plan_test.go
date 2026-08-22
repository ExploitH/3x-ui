package singboxadapter

import "testing"

func TestPlanManagedRelayUserStateDisablesOnlyTargetUser(t *testing.T) {
	fragment, err := BuildManagedRelayFragment(RelayFragmentInput{
		InboundTag: "managed-relay-hk1",
		ListenPort: 29001,
		Users: []ManagedRelayUser{
			{Email: "alice@example.com", UUID: "alice-uuid", ExitTag: "us2"},
			{Email: "bob@example.com", UUID: "bob-uuid", ExitTag: "us2"},
		},
		Exits: []ManagedRelayExit{{Tag: "us2", Server: "us2.example", ServerPort: 443}},
	})
	if err != nil {
		t.Fatal(err)
	}
	planned, err := PlanManagedRelayUserState(fragment, ManagedRelayUser{Email: "alice@example.com"}, false)
	if err != nil {
		t.Fatalf("disable plan: %v", err)
	}
	if len(planned.Inbounds) != 1 || len(planned.Inbounds[0].Users) != 1 || planned.Inbounds[0].Users[0].Email != "bob@example.com" {
		t.Fatalf("inbound users=%+v", planned.Inbounds)
	}
	if len(planned.RouteRules) != 1 || planned.RouteRules[0].AuthUser != "bob@example.com" {
		t.Fatalf("route rules=%+v", planned.RouteRules)
	}
}

func TestPlanManagedRelayUserStateEnableRequiresCanonicalCredentialSpec(t *testing.T) {
	fragment, err := BuildManagedRelayFragment(RelayFragmentInput{
		InboundTag: "managed-relay-hk1",
		ListenPort: 29001,
		Users:      []ManagedRelayUser{{Email: "bob@example.com", UUID: "bob-uuid", ExitTag: "us2"}},
		Exits:      []ManagedRelayExit{{Tag: "us2", Server: "us2.example", ServerPort: 443}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Alice is already disabled and absent from the fragment. It must not be
	// reconstructed from email alone.
	if _, err := PlanManagedRelayUserState(fragment, ManagedRelayUser{Email: "alice@example.com"}, true); err == nil {
		t.Fatal("enable without credential spec accepted")
	}
	planned, err := PlanManagedRelayUserState(fragment, ManagedRelayUser{Email: "alice@example.com", UUID: "alice-uuid", ExitTag: "us2"}, true)
	if err != nil {
		t.Fatalf("enable plan: %v", err)
	}
	if len(planned.Inbounds) != 1 || len(planned.Inbounds[0].Users) != 2 {
		t.Fatalf("restored users=%+v", planned.Inbounds)
	}
	if planned.Inbounds[0].Users[0].Email != "alice@example.com" {
		t.Fatalf("users not sorted=%+v", planned.Inbounds[0].Users)
	}
	if len(planned.RouteRules) != 2 {
		t.Fatalf("restored rules=%+v", planned.RouteRules)
	}
}

func TestPlanManagedRelayUserStateRejectsMissingExitOnEnable(t *testing.T) {
	fragment, err := BuildManagedRelayFragment(RelayFragmentInput{
		InboundTag: "managed-relay-hk1",
		ListenPort: 29001,
		Users:      []ManagedRelayUser{{Email: "bob@example.com", UUID: "bob-uuid", ExitTag: "us2"}},
		Exits:      []ManagedRelayExit{{Tag: "us2", Server: "us2.example", ServerPort: 443}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PlanManagedRelayUserState(fragment, ManagedRelayUser{Email: "alice@example.com", UUID: "alice-uuid", ExitTag: "missing"}, true); err == nil {
		t.Fatal("missing exit accepted")
	}
}
