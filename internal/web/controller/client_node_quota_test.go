package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

type clientNodeQuotaEnvelope struct {
	Success bool            `json:"success"`
	Msg     string          `json:"msg"`
	Obj     json.RawMessage `json:"obj"`
}

type clientNodeQuotaObject struct {
	NodeQuotas     []service.ClientNodeQuotaView `json:"nodeQuotas"`
	PendingNodeIds []int                         `json:"pendingNodeIds"`
	NodeId         int                           `json:"nodeId"`
	Pending        bool                          `json:"pending"`
}

func newClientNodeQuotaControllerTest(t *testing.T) (*gin.Engine, model.ClientRecord, model.Node) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	engine := gin.New()
	NewClientController(engine.Group("/panel/api/clients"))
	record := model.ClientRecord{Email: "quota-api@x", Enable: true}
	if err := database.GetDB().Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	node := model.Node{Name: "HK API", Address: "example.com", Port: 443, ApiToken: "tok", Enable: true}
	if err := database.GetDB().Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	return engine, record, node
}

func doClientNodeQuotaRequest(t *testing.T, engine *gin.Engine, method, path string, body any) clientNodeQuotaEnvelope {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s %s status=%d body=%s", method, path, w.Code, w.Body.String())
	}
	var env clientNodeQuotaEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, w.Body.String())
	}
	return env
}

func decodeClientNodeQuotaObject(t *testing.T, env clientNodeQuotaEnvelope) clientNodeQuotaObject {
	t.Helper()
	if !env.Success {
		t.Fatalf("request failed: %s", env.Msg)
	}
	var obj clientNodeQuotaObject
	if err := json.Unmarshal(env.Obj, &obj); err != nil {
		t.Fatalf("decode object: %v raw=%s", err, env.Obj)
	}
	return obj
}

func TestClientNodeQuotaControllerCRUDAndReset(t *testing.T) {
	engine, record, node := newClientNodeQuotaControllerTest(t)
	base := "/panel/api/clients/nodeQuotas/" + record.Email

	replaced := decodeClientNodeQuotaObject(t, doClientNodeQuotaRequest(t, engine, http.MethodPost, base, map[string]any{
		"nodeQuotas": []map[string]any{{
			"nodeId": node.Id, "totalBytes": 1000, "resetPolicy": "monthly", "resetDay": 15,
		}},
	}))
	if len(replaced.NodeQuotas) != 1 || replaced.NodeQuotas[0].NodeId != node.Id || replaced.NodeQuotas[0].TotalBytes != 1000 {
		t.Fatalf("replace response = %+v", replaced)
	}

	listed := decodeClientNodeQuotaObject(t, doClientNodeQuotaRequest(t, engine, http.MethodGet, base, nil))
	if len(listed.NodeQuotas) != 1 || listed.NodeQuotas[0].NodeName != "HK API" {
		t.Fatalf("list response = %+v", listed)
	}

	if err := database.GetDB().Create(&model.ClientNodeUsage{ClientId: record.Id, NodeId: node.Id, Up: 40, Down: 60}).Error; err != nil {
		t.Fatal(err)
	}
	reset := decodeClientNodeQuotaObject(t, doClientNodeQuotaRequest(t, engine, http.MethodPost, base+"/reset/"+strconv.Itoa(node.Id), nil))
	if reset.NodeId != node.Id || reset.Pending {
		t.Fatalf("specific reset response = %+v", reset)
	}
	if len(reset.NodeQuotas) != 1 || reset.NodeQuotas[0].Up != 0 || reset.NodeQuotas[0].Down != 0 {
		t.Fatalf("specific reset view = %+v", reset.NodeQuotas)
	}

	if err := database.GetDB().Model(&model.ClientNodeUsage{}).
		Where("client_id = ? AND node_id = ?", record.Id, node.Id).
		Updates(map[string]any{"up": 70, "down": 80}).Error; err != nil {
		t.Fatal(err)
	}
	resetAll := decodeClientNodeQuotaObject(t, doClientNodeQuotaRequest(t, engine, http.MethodPost, base+"/resetAll", nil))
	if len(resetAll.PendingNodeIds) != 0 || len(resetAll.NodeQuotas) != 1 || resetAll.NodeQuotas[0].Up != 0 || resetAll.NodeQuotas[0].Down != 0 {
		t.Fatalf("reset-all response = %+v", resetAll)
	}

	invalid := doClientNodeQuotaRequest(t, engine, http.MethodPost, base, map[string]any{
		"nodeQuotas": []map[string]any{{"nodeId": node.Id, "totalBytes": -1, "resetPolicy": "never", "resetDay": 1}},
	})
	if invalid.Success {
		t.Fatal("invalid negative quota was accepted")
	}
}
