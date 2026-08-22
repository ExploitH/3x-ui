package singboxadapter

import (
	"encoding/json"
	"testing"
)

func TestBuildTrafficPlanIgnoresUntrackedInbound(t *testing.T) {
	cfg := singboxConfig{
		Experimental: singboxExperimental{V2RayAPI: &singboxV2RayAPI{
			Listen: "127.0.0.1:19085",
			Stats: singboxStatsAPI{
				Enabled:  true,
				Users:    []string{"alice"},
				Inbounds: []string{"managed-in"},
			},
		}},
		Inbounds: []singboxInbound{
			{Tag: "managed-in", Users: []json.RawMessage{json.RawMessage(`{"name":"alice"}`)}},
			{Tag: "untracked-in", Users: []json.RawMessage{json.RawMessage(`{"name":"operator"}`)}},
		},
	}
	plan, err := buildTrafficPlan(cfg)
	if err != nil {
		t.Fatalf("buildTrafficPlan: %v", err)
	}
	if len(plan.inbounds) != 1 || plan.inbounds[0] != "managed-in" {
		t.Fatalf("inbounds=%v", plan.inbounds)
	}
	if len(plan.users) != 1 || plan.users[0] != "alice" {
		t.Fatalf("users=%v", plan.users)
	}
}
