package runtime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestRemoteFetchCapabilitiesCached(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/panel/api/server/capabilities" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"obj":{"mode":"managed","trafficSource":"sing-box-v2ray-api","clientCrud":true,"clientEnable":true,"perClientTraffic":true}}`))
	}))
	defer server.Close()

	r := NewRemote(nodeForPlainServer(t, server, "verify", "tok"), nil)
	first, err := r.FetchCapabilitiesCached(context.Background())
	if err != nil {
		t.Fatalf("first FetchCapabilitiesCached: %v", err)
	}
	second, err := r.FetchCapabilitiesCached(context.Background())
	if err != nil {
		t.Fatalf("second FetchCapabilitiesCached: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("capability calls=%d, want 1 within cache TTL", calls.Load())
	}
	if first.Mode != "managed" || second.Mode != "managed" {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}

func TestRemoteManagedSingboxTrafficRejectsNonSingboxSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"obj":{"mode":"managed","trafficSource":"xray-api","clientCrud":true,"clientEnable":true,"perClientTraffic":true}}`))
	}))
	defer server.Close()

	r := NewRemote(nodeForPlainServer(t, server, "verify", "tok"), nil)
	if _, err := r.FetchManagedSingboxTrafficSnapshot(context.Background()); !errors.Is(err, ErrManagedSingboxTrafficUnsupported) {
		t.Fatalf("error=%v, want ErrManagedSingboxTrafficUnsupported", err)
	}
}
