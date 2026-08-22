package singboxadapter

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestReadOnlyHandlerListsInboundsAndReportsStatus(t *testing.T) {
	config := []byte(`{"inbounds":[{"type":"vless","tag":"hk3-reality","listen":"::","listen_port":8881,"users":[{"uuid":"redacted"}]},{"type":"hysteria2","tag":"hk3-hy2","listen":"::","listen_port":8882,"users":[{"password":"redacted"}]}]}`)
	h, err := NewReadOnlyHandler(ReadOnlyOptions{
		ConfigJSON: config,
		Token:      "adapter-test-token",
		BasePath:   "/adapter/",
	})
	if err != nil {
		t.Fatalf("NewReadOnlyHandler: %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/adapter/panel/api/inbounds/list", nil)
	listReq.Header.Set("Authorization", "Bearer adapter-test-token")
	listResp := httptest.NewRecorder()
	h.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	if got := listResp.Body.String(); !containsAll(got, `"tag":"hk3-reality"`, `"port":8881`, `"tag":"hk3-hy2"`, `"port":8882`) {
		t.Fatalf("list body=%s", got)
	}
	if containsAny(listResp.Body.String(), "redacted", "uuid", "password") {
		t.Fatalf("list leaked user credentials: %s", listResp.Body.String())
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/adapter/panel/api/server/status", nil)
	statusReq.Header.Set("Authorization", "Bearer adapter-test-token")
	statusResp := httptest.NewRecorder()
	h.ServeHTTP(statusResp, statusReq)
	if statusResp.Code != http.StatusOK || !containsAll(statusResp.Body.String(), `"success":true`, `"state":"not-applicable"`, `"panelVersion":"singbox-adapter-readonly"`) {
		t.Fatalf("status=%d body=%s", statusResp.Code, statusResp.Body.String())
	}
}

func TestReadOnlyHandlerRejectsMissingOrWrongTokenAndAllMutations(t *testing.T) {
	h, err := NewReadOnlyHandler(ReadOnlyOptions{ConfigJSON: []byte(`{"inbounds":[]}`), Token: "secret", BasePath: "/adapter/"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		method string
		target string
		token  string
		want   int
	}{
		{"missing token", http.MethodGet, "/adapter/panel/api/inbounds/list", "", http.StatusUnauthorized},
		{"wrong token", http.MethodGet, "/adapter/panel/api/inbounds/list", "wrong", http.StatusUnauthorized},
		{"mutation denied", http.MethodPost, "/adapter/panel/api/inbounds/add", "secret", http.StatusMethodNotAllowed},
		{"outside base path", http.MethodGet, "/panel/api/server/status", "secret", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			resp := httptest.NewRecorder()
			h.ServeHTTP(resp, req)
			if resp.Code != tc.want {
				t.Fatalf("status=%d body=%s want %d", resp.Code, resp.Body.String(), tc.want)
			}
		})
	}
}

func TestReadOnlyHandlerReportsCapabilities(t *testing.T) {
	h, err := NewReadOnlyHandler(ReadOnlyOptions{ConfigJSON: []byte(`{"inbounds":[]}`), Token: "secret", BasePath: "/adapter/"})
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
	if !containsAll(body,
		`"mode":"readonly"`,
		`"config":true`,
		`"inboundInventory":true`,
		`"clientCrud":false`,
		`"clientEnable":false`,
		`"perClientTraffic":false`,
		`"trafficReset":false`,
		`"clientIp":false`,
		`"relayIdentity":false`,
	) {
		t.Fatalf("capabilities body=%s", body)
	}
}

func TestCapabilitiesReturnsUnavailableAfterConfigCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	valid := []byte(`{"inbounds":[{"type":"vless","tag":"stable","listen_port":443}]}`)
	if err := os.WriteFile(path, valid, 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewReadOnlyHandler(ReadOnlyOptions{ConfigPath: path, Token: "secret", BasePath: "/adapter/"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"inbounds":`), 0o600); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/adapter/panel/api/server/capabilities", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestReadOnlyHandlerRejectsMalformedConfigAndDuplicateTags(t *testing.T) {
	for _, config := range [][]byte{
		[]byte(`{"inbounds":`),
		[]byte(`{"inbounds":[{"type":"vless","tag":"same","listen_port":1},{"type":"vless","tag":"same","listen_port":2}]}`),
	} {
		if _, err := NewReadOnlyHandler(ReadOnlyOptions{ConfigJSON: config, Token: "secret", BasePath: "/adapter/"}); err == nil {
			t.Fatalf("invalid config accepted: %s", config)
		}
	}
}

func containsAll(value string, needles ...string) bool {
	for _, needle := range needles {
		if !contains(value, needle) {
			return false
		}
	}
	return true
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if contains(value, needle) {
			return true
		}
	}
	return false
}

func contains(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
