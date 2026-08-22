package singboxadapter

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManagedClientUpdateFailsClosedWhenStateStoreMissing(t *testing.T) {
	config := []byte(`{"inbounds":[],"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct"}}`)
	h, err := NewReadOnlyHandler(ReadOnlyOptions{
		ConfigJSON:          config,
		Token:               "adapter-test-token",
		BasePath:            "/adapter/",
		ManagedRelayMutator: &ManagedRelayMutator{},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	req := httptest.NewRequest(http.MethodPost, "/adapter/panel/api/clients/update/alice%40example.com?inboundIds=1", strings.NewReader(`{"email":"alice@example.com","enable":false}`))
	req.Header.Set("Authorization", "Bearer adapter-test-token")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusServiceUnavailable || !strings.Contains(resp.Body.String(), "state store is unavailable") {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}
