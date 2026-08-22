package singboxadapter

import "testing"

func TestMergeManagedRelayFragmentPreservesLegacyConfigAndReplacesOnlyManagedTags(t *testing.T) {
	legacy := map[string]any{
		"inbounds": []any{
			map[string]any{"type": "hysteria2", "tag": "legacy-relay", "listen_port": 28882},
		},
		"outbounds": []any{
			map[string]any{"type": "direct", "tag": "direct"},
			map[string]any{"type": "hysteria2", "tag": "managed-exit-old", "server": "old.example"},
		},
		"route": map[string]any{"rules": []any{
			map[string]any{"outbound": "direct"},
			map[string]any{"domain_suffix": []any{"example.com"}, "outbound": "legacy-relay"},
		}},
	}
	fragment, err := BuildManagedRelayFragment(RelayFragmentInput{
		InboundTag: "managed-relay-new", ListenPort: 29001,
		Users: []ManagedRelayUser{{Email: "alice", UUID: "u", ExitTag: "us2"}},
		Exits: []ManagedRelayExit{{Tag: "us2", Server: "new.example", ServerPort: 443}},
	})
	if err != nil {
		t.Fatal(err)
	}
	merged, err := MergeManagedRelayFragment(legacy, fragment)
	if err != nil {
		t.Fatal(err)
	}
	inbounds := merged["inbounds"].([]any)
	if len(inbounds) != 2 || inbounds[0].(map[string]any)["tag"] != "legacy-relay" || inbounds[1].(map[string]any)["tag"] != "managed-relay-new" {
		t.Fatalf("inbounds=%#v", inbounds)
	}
	outbounds := merged["outbounds"].([]any)
	if len(outbounds) != 2 {
		t.Fatalf("outbounds=%#v", outbounds)
	}
	for _, raw := range outbounds {
		row := raw.(map[string]any)
		if row["tag"] == "managed-exit-old" {
			t.Fatalf("stale managed outbound survived: %#v", row)
		}
	}
	route := merged["route"].(map[string]any)
	rules := route["rules"].([]any)
	if len(rules) != 3 {
		t.Fatalf("route rules=%#v", rules)
	}
	firstRule := rules[0].(map[string]any)
	if firstRule["outbound"] != "managed-us2" || firstRule["auth_user"] != "alice" {
		t.Fatalf("managed rule did not precede legacy catch-all: %#v", firstRule)
	}
	inboundRule, ok := firstRule["inbound"].([]any)
	if !ok || len(inboundRule) != 1 || inboundRule[0] != "managed-relay-new" {
		t.Fatalf("managed rule inbound scope=%#v", firstRule)
	}
	if rules[1].(map[string]any)["outbound"] != "direct" || rules[2].(map[string]any)["outbound"] != "legacy-relay" {
		t.Fatalf("legacy rule order changed: %#v", rules)
	}
}
