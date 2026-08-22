package singboxadapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestManagedClientUpdateEndpointIsOptInAndNodeScoped(t *testing.T) {
	mutator, _, statePath := writeMutatorFixture(t)
	h, err := NewReadOnlyHandler(ReadOnlyOptions{
		ConfigPath:          mutator.Config.path,
		Token:               "adapter-test-token",
		BasePath:            "/adapter/",
		ManagedRelayMutator: mutator,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	capReq := httptest.NewRequest(http.MethodGet, "/adapter/panel/api/server/capabilities", nil)
	capReq.Header.Set("Authorization", "Bearer adapter-test-token")
	capResp := httptest.NewRecorder()
	h.ServeHTTP(capResp, capReq)
	if capResp.Code != http.StatusOK || !strings.Contains(capResp.Body.String(), `"clientEnable":true`) || strings.Contains(capResp.Body.String(), `"clientCrud":true`) {
		t.Fatalf("capabilities status=%d body=%s", capResp.Code, capResp.Body.String())
	}
	state, err := NewManagedRelayStateStore(statePath).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	inboundID := stableInboundID(state.InboundTag)
	req := httptest.NewRequest(http.MethodPost, "/adapter/panel/api/clients/update/alice%40example.com?inboundIds="+strconv.Itoa(inboundID), strings.NewReader(`{"email":"alice@example.com","enable":false}`))
	req.Header.Set("Authorization", "Bearer adapter-test-token")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || strings.Contains(resp.Body.String(), "alice-secret") {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	state, err = NewManagedRelayStateStore(statePath).Load(context.Background())
	if err != nil || state.Users[0].Enabled {
		t.Fatalf("state=%+v err=%v", state, err)
	}

	badReq := httptest.NewRequest(http.MethodPost, "/adapter/panel/api/clients/update/alice%40example.com?inboundIds=999", strings.NewReader(`{"email":"alice@example.com","enable":true}`))
	badReq.Header.Set("Authorization", "Bearer adapter-test-token")
	badResp := httptest.NewRecorder()
	h.ServeHTTP(badResp, badReq)
	if badResp.Code != http.StatusBadRequest {
		t.Fatalf("wrong inbound status=%d body=%s", badResp.Code, badResp.Body.String())
	}
	getReq := httptest.NewRequest(http.MethodGet, "/adapter/panel/api/clients/update/alice%40example.com?inboundIds="+strconv.Itoa(inboundID), nil)
	getReq.Header.Set("Authorization", "Bearer adapter-test-token")
	getResp := httptest.NewRecorder()
	h.ServeHTTP(getResp, getReq)
	if getResp.Code != http.StatusNotFound {
		t.Fatalf("GET mutation status=%d body=%s", getResp.Code, getResp.Body.String())
	}
}

func TestManagedClientUpdateIsRejectedByDefaultReadonlyHandler(t *testing.T) {
	config := []byte(`{"inbounds":[],"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct"}}`)
	h, err := NewReadOnlyHandler(ReadOnlyOptions{ConfigJSON: config, Token: "adapter-test-token", BasePath: "/adapter/"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	req := httptest.NewRequest(http.MethodPost, "/adapter/panel/api/clients/update/alice%40example.com?inboundIds=1", strings.NewReader(`{"email":"alice@example.com","enable":false}`))
	req.Header.Set("Authorization", "Bearer adapter-test-token")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestParseInboundIDsRejectsMalformedLists(t *testing.T) {
	for _, raw := range []string{"", "0", "1,x", "-1"} {
		if _, err := parseInboundIDs(raw); err == nil {
			t.Fatalf("accepted malformed inboundIds=%q", raw)
		}
	}
}
