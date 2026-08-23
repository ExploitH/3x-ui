package singboxadapter

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestReadOnlyHandlerDirectMutationScopesAndPreservesQuotaReason(t *testing.T) {
	m := directTestMutator(t)
	h, err := NewReadOnlyHandler(ReadOnlyOptions{
		ConfigPath: m.Config.path, Token: "secret", BasePath: "/adapter/", DirectInboundMutator: m,
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{strconv.Itoa(stableInboundID("direct-vless")), strconv.Itoa(stableInboundID("direct-hy2")), strconv.Itoa(stableInboundID("direct-tuic"))}
	body := []byte(`{"email":"alice@example.com","enable":false}`)
	req := httptest.NewRequest(http.MethodPost, "/adapter/panel/api/clients/update/alice@example.com?inboundIds="+ids[0]+","+ids[1]+","+ids[2], bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-3x-UI-Mutation-Reason", "quota-block")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("block status=%d body=%s", resp.Code, resp.Body.String())
	}
	for _, tag := range m.InboundTags {
		if got := directConfigUsers(t, m.Config.path)[tag]; got != 0 {
			t.Fatalf("blocked %s users=%d", tag, got)
		}
	}

	reset := httptest.NewRequest(http.MethodPost, "/adapter/panel/api/clients/update/alice@example.com?inboundIds="+ids[0]+","+ids[1]+","+ids[2], bytes.NewReader([]byte(`{"email":"alice@example.com","enable":true}`)))
	reset.Header.Set("Authorization", "Bearer secret")
	reset.Header.Set("X-3x-UI-Mutation-Reason", "quota-reset")
	resetResp := httptest.NewRecorder()
	h.ServeHTTP(resetResp, reset)
	if resetResp.Code != http.StatusOK {
		t.Fatalf("reset status=%d body=%s", resetResp.Code, resetResp.Body.String())
	}
	for _, tag := range m.InboundTags {
		if got := directConfigUsers(t, m.Config.path)[tag]; got != 1 {
			t.Fatalf("reset %s users=%d", tag, got)
		}
	}
}

func TestReadOnlyHandlerDirectCapabilitiesExposeEnableOnly(t *testing.T) {
	m := directTestMutator(t)
	h, err := NewReadOnlyHandler(ReadOnlyOptions{ConfigPath: m.Config.path, Token: "secret", BasePath: "/adapter/", DirectInboundMutator: m})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/adapter/panel/api/server/capabilities", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !containsAll(body, `"mode":"managed-direct"`, `"clientCrud":false`, `"clientEnable":true`) {
		t.Fatalf("body=%s", body)
	}
}
