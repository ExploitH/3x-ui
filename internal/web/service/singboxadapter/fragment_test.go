package singboxadapter

import (
	"encoding/json"
	"testing"
)

func TestBuildManagedRelayFragmentCreatesPerUserInboundAndExitOutbounds(t *testing.T) {
	fragment, err := BuildManagedRelayFragment(RelayFragmentInput{
		InboundTag: "managed-relay-hk1",
		ListenPort: 29001,
		Users: []ManagedRelayUser{
			{Email: "alice@example.com", UUID: "alice-uuid", ExitTag: "exit-us2"},
			{Email: "bob@example.com", UUID: "bob-uuid", ExitTag: "exit-us2"},
		},
		Exits: []ManagedRelayExit{{Tag: "exit-us2", Server: "us2.example", ServerPort: 443}},
	})
	if err != nil {
		t.Fatalf("BuildManagedRelayFragment: %v", err)
	}
	if len(fragment.Inbounds) != 1 || len(fragment.Outbounds) != 2 || len(fragment.RouteRules) != 2 {
		t.Fatalf("fragment sizes: inbounds=%d outbounds=%d rules=%d", len(fragment.Inbounds), len(fragment.Outbounds), len(fragment.RouteRules))
	}
	if fragment.Inbounds[0].Tag != "managed-relay-hk1" || len(fragment.Inbounds[0].Users) != 2 {
		t.Fatalf("inbound=%+v", fragment.Inbounds[0])
	}
	if fragment.RouteRules[0].AuthUser != "alice@example.com" || fragment.RouteRules[0].Outbound != "managed-exit-us2" {
		t.Fatalf("route rule=%+v", fragment.RouteRules[0])
	}
	if fragment.Outbounds[0].Tag != "direct" || fragment.Outbounds[1].Tag != "managed-exit-us2" {
		t.Fatalf("outbounds=%+v", fragment.Outbounds)
	}
	if _, err := json.Marshal(fragment); err != nil {
		t.Fatal(err)
	}
}

func TestBuildManagedRelayFragmentRejectsDuplicateUsersAndInvalidExit(t *testing.T) {
	cases := []RelayFragmentInput{
		{InboundTag: "x", ListenPort: 1, Users: []ManagedRelayUser{{Email: "a", UUID: "same", ExitTag: "missing"}}},
		{InboundTag: "x", ListenPort: 1, Users: []ManagedRelayUser{{Email: "a", UUID: "same", ExitTag: "e"}, {Email: "a", UUID: "other", ExitTag: "e"}}, Exits: []ManagedRelayExit{{Tag: "e", Server: "x", ServerPort: 1}}},
		{InboundTag: "x", ListenPort: 1, Users: []ManagedRelayUser{{Email: "a", UUID: "same", ExitTag: "e"}, {Email: "b", UUID: "same", ExitTag: "e"}}, Exits: []ManagedRelayExit{{Tag: "e", Server: "x", ServerPort: 1}}},
	}
	for _, input := range cases {
		if _, err := BuildManagedRelayFragment(input); err == nil {
			t.Fatalf("invalid adapter input accepted: %+v", input)
		}
	}
}
