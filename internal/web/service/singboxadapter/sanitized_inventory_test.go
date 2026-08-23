package singboxadapter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadOnlyInboundListReturnsSanitizedClientSettings(t *testing.T) {
	config := []byte(`{"inbounds":[{"type":"hysteria2","tag":"managed-relay-hk1","listen":"::","listen_port":29001,"users":[{"name":"alice@example.com","password":"secret-not-for-api"}]}]}`)
	h, err := NewReadOnlyHandler(ReadOnlyOptions{ConfigJSON: config, Token: "adapter-test-token", BasePath: "/adapter/"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	req := httptest.NewRequest(http.MethodGet, "/adapter/panel/api/inbounds/list", nil)
	req.Header.Set("Authorization", "Bearer adapter-test-token")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), "secret-not-for-api") || strings.Contains(resp.Body.String(), `"password"`) {
		t.Fatalf("credentials leaked: %s", resp.Body.String())
	}
	var envelope struct {
		Obj []ReadOnlyInbound `json:"obj"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Obj) != 1 || !strings.Contains(envelope.Obj[0].Settings, `"email":"alice@example.com"`) || !strings.Contains(envelope.Obj[0].Settings, `"enable":true`) {
		t.Fatalf("sanitized settings=%q", envelope.Obj[0].Settings)
	}
}
