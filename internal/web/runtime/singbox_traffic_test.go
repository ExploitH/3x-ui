package runtime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoteFetchSingboxTrafficSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/panel/api/traffic/snapshot" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"obj":{"source":"sing-box-v2ray-api","capturedAt":"2026-08-22T20:01:18Z","users":[{"name":"alice","uplink":104,"downlink":713,"total":817}],"inbounds":[{"tag":"hk3-reality","uplink":104,"downlink":713,"total":817}]}}`))
	}))
	defer server.Close()

	r := NewRemote(nodeForPlainServer(t, server, "verify", "tok"), nil)
	snapshot, err := r.FetchSingboxTrafficSnapshot(context.Background())
	if err != nil {
		t.Fatalf("FetchSingboxTrafficSnapshot: %v", err)
	}
	if snapshot.Source != "sing-box-v2ray-api" || len(snapshot.Users) != 1 || snapshot.Users[0].Name != "alice" || snapshot.Users[0].Total != 817 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestRemoteFetchSingboxTrafficSnapshot404IsUnsupported(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	r := NewRemote(nodeForPlainServer(t, server, "verify", "tok"), nil)
	_, err := r.FetchSingboxTrafficSnapshot(context.Background())
	if !errors.Is(err, ErrSingboxTrafficUnsupported) {
		t.Fatalf("error=%v, want ErrSingboxTrafficUnsupported", err)
	}
}

func TestRemoteFetchSingboxTrafficSnapshotRejectsInconsistentTotal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"obj":{"source":"sing-box-v2ray-api","capturedAt":"2026-08-22T20:01:18Z","users":[{"name":"alice","uplink":104,"downlink":713,"total":999}],"inbounds":[]}}`))
	}))
	defer server.Close()

	r := NewRemote(nodeForPlainServer(t, server, "verify", "tok"), nil)
	_, err := r.FetchSingboxTrafficSnapshot(context.Background())
	if err == nil || !strings.Contains(err.Error(), "total") {
		t.Fatalf("error=%v, want total validation error", err)
	}
}
