package singboxadapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeTrafficStatsProvider struct {
	stats    []TrafficStat
	patterns []string
}

func (f *fakeTrafficStatsProvider) Query(_ context.Context, patterns []string) ([]TrafficStat, error) {
	f.patterns = append([]string(nil), patterns...)
	return append([]TrafficStat(nil), f.stats...), nil
}

func TestReadOnlyHandlerReportsTrafficSnapshotWithoutReset(t *testing.T) {
	config := []byte(`{
		"experimental":{"v2ray_api":{"listen":"127.0.0.1:19085","stats":{"enabled":true,"users":["alice"],"inbounds":["hk3-reality"]}}},
		"inbounds":[{"type":"vless","tag":"hk3-reality","listen":"::","listen_port":8881,"users":[{"name":"alice","uuid":"redacted"}]}]
	}`)
	provider := &fakeTrafficStatsProvider{stats: []TrafficStat{
		{Name: "user>>>alice>>>traffic>>>uplink", Value: 123},
		{Name: "user>>>alice>>>traffic>>>downlink", Value: 456},
		{Name: "inbound>>>hk3-reality>>>traffic>>>uplink", Value: 1000},
		{Name: "inbound>>>hk3-reality>>>traffic>>>downlink", Value: 2000},
	}}
	h, err := NewReadOnlyHandler(ReadOnlyOptions{
		ConfigJSON:    config,
		Token:         "adapter-test-token",
		BasePath:      "/adapter/",
		StatsProvider: provider,
	})
	if err != nil {
		t.Fatalf("NewReadOnlyHandler: %v", err)
	}
	defer h.Close()

	capReq := httptest.NewRequest(http.MethodGet, "/adapter/panel/api/server/capabilities", nil)
	capReq.Header.Set("Authorization", "Bearer adapter-test-token")
	capResp := httptest.NewRecorder()
	h.ServeHTTP(capResp, capReq)
	if capResp.Code != http.StatusOK || !containsAll(capResp.Body.String(), `"mode":"traffic-readonly"`, `"trafficSource":"sing-box-v2ray-api"`, `"perClientTraffic":true`, `"clientCrud":false`, `"clientEnable":false`) {
		t.Fatalf("capabilities=%d body=%s", capResp.Code, capResp.Body.String())
	}

	snapshotReq := httptest.NewRequest(http.MethodGet, "/adapter/panel/api/traffic/snapshot", nil)
	snapshotReq.Header.Set("Authorization", "Bearer adapter-test-token")
	snapshotResp := httptest.NewRecorder()
	h.ServeHTTP(snapshotResp, snapshotReq)
	if snapshotResp.Code != http.StatusOK {
		t.Fatalf("snapshot status=%d body=%s", snapshotResp.Code, snapshotResp.Body.String())
	}
	body := snapshotResp.Body.String()
	if !containsAll(body,
		`"source":"sing-box-v2ray-api"`,
		`"name":"alice"`,
		`"uplink":123`,
		`"downlink":456`,
		`"total":579`,
		`"tag":"hk3-reality"`,
		`"uplink":1000`,
		`"downlink":2000`,
		`"total":3000`,
	) {
		t.Fatalf("snapshot body=%s", body)
	}
	if len(provider.patterns) != 4 || !strings.Contains(provider.patterns[0], "user>>>") || !strings.Contains(provider.patterns[1], "user>>>") || !strings.Contains(provider.patterns[2], "inbound>>>") || !strings.Contains(provider.patterns[3], "inbound>>>") {
		t.Fatalf("query patterns=%v", provider.patterns)
	}
}

func TestReadOnlyHandlerRejectsTrafficSnapshotWhenUserNameMissing(t *testing.T) {
	config := []byte(`{
		"experimental":{"v2ray_api":{"listen":"127.0.0.1:19085","stats":{"enabled":true,"users":["alice"],"inbounds":["hk3-reality"]}}},
		"inbounds":[{"type":"vless","tag":"hk3-reality","listen":"::","listen_port":8881,"users":[{"uuid":"redacted"}]}]
	}`)
	provider := &fakeTrafficStatsProvider{}
	h, err := NewReadOnlyHandler(ReadOnlyOptions{ConfigJSON: config, Token: "secret", StatsProvider: provider})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	req := httptest.NewRequest(http.MethodGet, "/adapter/panel/api/traffic/snapshot", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusServiceUnavailable || !strings.Contains(resp.Body.String(), "user name") {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if provider.patterns != nil {
		t.Fatal("stats provider was called despite missing user identity")
	}
}

func TestReadOnlyHandlerRejectsUnavailableTrafficCounters(t *testing.T) {
	config := []byte(`{
		"experimental":{"v2ray_api":{"listen":"127.0.0.1:19085","stats":{"enabled":true,"users":["alice"],"inbounds":["hk3-reality"]}}},
		"inbounds":[{"type":"vless","tag":"hk3-reality","listen":"::","listen_port":8881,"users":[{"name":"alice","uuid":"redacted"}]}]
	}`)
	provider := &fakeTrafficStatsProvider{}
	h, err := NewReadOnlyHandler(ReadOnlyOptions{ConfigJSON: config, Token: "secret", StatsProvider: provider})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	req := httptest.NewRequest(http.MethodGet, "/adapter/panel/api/traffic/snapshot", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusServiceUnavailable || !strings.Contains(resp.Body.String(), "counter unavailable") {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestReadOnlyHandlerRejectsPartialTrafficCounters(t *testing.T) {
	config := []byte(`{
		"experimental":{"v2ray_api":{"listen":"127.0.0.1:19085","stats":{"enabled":true,"users":["alice"],"inbounds":["hk3-reality"]}}},
		"inbounds":[{"type":"vless","tag":"hk3-reality","listen":"::","listen_port":8881,"users":[{"name":"alice","uuid":"redacted"}]}]
	}`)
	provider := &fakeTrafficStatsProvider{stats: []TrafficStat{
		{Name: "user>>>alice>>>traffic>>>uplink", Value: 10},
		{Name: "inbound>>>hk3-reality>>>traffic>>>uplink", Value: 10},
	}}
	h, err := NewReadOnlyHandler(ReadOnlyOptions{ConfigJSON: config, Token: "secret", StatsProvider: provider})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	req := httptest.NewRequest(http.MethodGet, "/adapter/panel/api/traffic/snapshot", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusServiceUnavailable || !strings.Contains(resp.Body.String(), "counter unavailable") {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}
