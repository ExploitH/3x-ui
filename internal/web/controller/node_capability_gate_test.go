package controller

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestNodeControllerAddRejectsReadonlyCapabilityBeforeCreate(t *testing.T) {
	engine := newNodeCredentialTestEngine(t)

	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/panel/api/server/capabilities" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"obj": map[string]any{
				"mode":             "readonly",
				"config":           true,
				"inboundInventory": true,
				"perClientTraffic": false,
			},
		})
	}))
	defer remote.Close()

	host, portString, err := net.SplitHostPort(strings.TrimPrefix(remote.URL, "http://"))
	if err != nil {
		t.Fatalf("split remote address: %v", err)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		t.Fatalf("parse remote port: %v", err)
	}
	payload := map[string]any{
		"name":                "readonly-add",
		"scheme":              "http",
		"address":             host,
		"port":                port,
		"basePath":            "/",
		"apiToken":            "node-token",
		"enable":              true,
		"allowPrivateAddress": true,
		"tlsVerifyMode":       "skip",
	}
	raw, _ := json.Marshal(payload)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/panel/api/nodes/add", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if !strings.Contains(w.Body.String(), `"success":false`) || !strings.Contains(w.Body.String(), "per-client traffic") {
		t.Fatalf("add response=%s, want capability rejection", w.Body.String())
	}
	var count int64
	if err := database.GetDB().Model(&model.Node{}).Where("name = ?", "readonly-add").Count(&count).Error; err != nil {
		t.Fatalf("count created node: %v", err)
	}
	if count != 0 {
		t.Fatalf("readonly node rows=%d, want 0", count)
	}
}
