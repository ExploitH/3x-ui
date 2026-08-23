package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteSingboxTrafficSourceAllowsReadonlyAccounting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/panel/api/server/capabilities":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "obj": map[string]any{
				"mode": "traffic-readonly", "trafficSource": "sing-box-v2ray-api",
				"clientCrud": false, "clientEnable": false, "perClientTraffic": true,
			}})
		case "/panel/api/traffic/snapshot":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "obj": map[string]any{
				"source":     "sing-box-v2ray-api",
				"capturedAt": "2026-08-23T00:00:00Z",
				"users":      []map[string]any{{"name": "alice@example.com", "uplink": 11, "downlink": 17, "total": 28}},
				"inbounds":   []map[string]any{{"tag": "hk3-reality", "uplink": 11, "downlink": 17, "total": 28}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	r := NewRemote(nodeForPlainServer(t, server, "verify", "tok"), nil)
	snapshot, err := r.FetchSingboxTrafficSource(context.Background())
	if err != nil {
		t.Fatalf("FetchSingboxTrafficSource: %v", err)
	}
	if snapshot == nil || len(snapshot.Users) != 1 || snapshot.Users[0].Total != 28 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestRemoteSingboxTrafficSourceRejectsMissingPerClientCapability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"obj":{"mode":"traffic-readonly","trafficSource":"sing-box-v2ray-api","perClientTraffic":false}}`))
	}))
	defer server.Close()

	r := NewRemote(nodeForPlainServer(t, server, "verify", "tok"), nil)
	if _, err := r.FetchSingboxTrafficSource(context.Background()); !errors.Is(err, ErrSingboxTrafficUnsupported) {
		t.Fatalf("error=%v, want ErrSingboxTrafficUnsupported", err)
	}
}
