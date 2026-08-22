package service

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
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

func TestUpdateFromRequestRejectsReadonlyNodeEnable(t *testing.T) {
	setupConflictDB(t)

	var capabilityCalls int
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/panel/api/server/capabilities" {
			t.Fatalf("unexpected remote path: %s", r.URL.Path)
		}
		capabilityCalls++
		if got := r.Header.Get("Authorization"); got != "Bearer node-token" {
			t.Fatalf("authorization=%q", got)
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
	node := &model.Node{
		Name:                "readonly-canary",
		Scheme:              "http",
		Address:             host,
		Port:                port,
		BasePath:            "/",
		ApiToken:            "node-token",
		Enable:              false,
		AllowPrivateAddress: true,
		TlsVerifyMode:       "skip",
	}
	if err := database.GetDB().Create(node).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}
	if err := database.GetDB().Model(&model.Node{}).Where("id = ?", node.Id).Update("enable", false).Error; err != nil {
		t.Fatalf("disable seeded node: %v", err)
	}

	previous := runtime.GetManager()
	runtime.SetManager(runtime.NewManager(runtime.LocalDeps{APIPort: func() int { return 0 }}))
	t.Cleanup(func() { runtime.SetManager(previous) })

	requestToken := "node-token"
	err = (&NodeService{}).UpdateFromRequest(node.Id, &NodeMutationRequest{
		Name:                node.Name,
		Scheme:              "http",
		Address:             host,
		Port:                port,
		BasePath:            "/",
		ApiToken:            &requestToken,
		Enable:              true,
		AllowPrivateAddress: true,
		TlsVerifyMode:       "skip",
	})
	if err == nil || !strings.Contains(err.Error(), "per-client traffic") {
		t.Fatalf("UpdateFromRequest error=%v, want per-client traffic gate", err)
	}
	if capabilityCalls != 1 {
		t.Fatalf("capability calls=%d, want 1", capabilityCalls)
	}

	var stored model.Node
	if err := database.GetDB().First(&stored, node.Id).Error; err != nil {
		t.Fatalf("reload node: %v", err)
	}
	if stored.Enable {
		t.Fatal("readonly node was enabled despite capability gate")
	}
}

func TestUpdateUsesStoredTokenWhenEnablingDisabledNode(t *testing.T) {
	setupConflictDB(t)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/panel/api/server/capabilities" {
			t.Fatalf("unexpected remote path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"obj":{"mode":"managed","clientCrud":true,"clientEnable":true,"perClientTraffic":true}}`))
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
	node := &model.Node{
		Name:                "stored-token-disabled",
		Scheme:              "http",
		Address:             host,
		Port:                port,
		BasePath:            "/",
		ApiToken:            "stored-token",
		Enable:              false,
		AllowPrivateAddress: true,
		TlsVerifyMode:       "skip",
	}
	if err := database.GetDB().Create(node).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}
	if err := database.GetDB().Model(&model.Node{}).Where("id = ?", node.Id).Update("enable", false).Error; err != nil {
		t.Fatalf("disable seeded node: %v", err)
	}
	previous := runtime.GetManager()
	runtime.SetManager(runtime.NewManager(runtime.LocalDeps{APIPort: func() int { return 0 }}))
	t.Cleanup(func() { runtime.SetManager(previous) })

	if err := (&NodeService{}).Update(node.Id, &model.Node{
		Name:                node.Name,
		Scheme:              "http",
		Address:             host,
		Port:                port,
		BasePath:            "/",
		Enable:              true,
		AllowPrivateAddress: true,
		TlsVerifyMode:       "skip",
	}); err != nil {
		t.Fatalf("Update with stored token: %v", err)
	}
	var stored model.Node
	if err := database.GetDB().First(&stored, node.Id).Error; err != nil {
		t.Fatalf("reload node: %v", err)
	}
	if !stored.Enable {
		t.Fatal("managed node was not enabled")
	}
}
