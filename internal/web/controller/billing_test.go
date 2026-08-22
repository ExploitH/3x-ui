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
)

type billingControllerEnvelope struct {
	Success bool            `json:"success"`
	Msg     string          `json:"msg"`
	Obj     json.RawMessage `json:"obj"`
}

func newBillingControllerTest(t *testing.T) (*gin.Engine, model.Node) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, "x-ui.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })
	node := model.Node{Name: "Billing API Node", Address: "example.com", Port: 443, ApiToken: "tok", Enable: true}
	if err := database.GetDB().Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	NewBillingController(engine.Group("/panel/api/billing"))
	return engine, node
}

func doBillingControllerRequest(t *testing.T, engine *gin.Engine, method, path string, body any) billingControllerEnvelope {
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
	var env billingControllerEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode response: %v body=%s", err, w.Body.String())
	}
	return env
}

func TestBillingControllerImportCyclesAndReadSummary(t *testing.T) {
	engine, node := newBillingControllerTest(t)
	db := database.GetDB()
	if err := db.Create(&model.NodeCostProfile{
		NodeId: node.Id, Provider: "test", AmountMinor: 100, Currency: "CNY", BillingCycle: "monthly",
		IncludedTrafficBytes: 10 * (1 << 30), OveragePriceMinorPerGB: 2, EffectiveFrom: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.NodeTrafficCycle{
		NodeId: node.Id, CycleStart: 100, CycleEnd: 200, Source: "heartbeat", IngressBytes: 999,
	}).Error; err != nil {
		t.Fatal(err)
	}
	imported := doBillingControllerRequest(t, engine, http.MethodPost, "/panel/api/billing/cycles", map[string]any{
		"nodeId": node.Id, "cycleStart": 100, "cycleEnd": 200,
		"ingressBytes": 11, "egressBytes": 22, "providerBillableBytes": 12 * (1 << 30),
		"source": "provider_import", "trafficCalcType": "total",
	})
	if !imported.Success {
		t.Fatalf("import failed: %s", imported.Msg)
	}
	var importedObj map[string]any
	if err := json.Unmarshal(imported.Obj, &importedObj); err != nil {
		t.Fatal(err)
	}
	if importedObj["cycleStart"] != float64(100) || importedObj["source"] != "provider_import" {
		t.Fatalf("import response=%+v", importedObj)
	}

	cycles := doBillingControllerRequest(t, engine, http.MethodGet, "/panel/api/billing/cycles/"+itoa(node.Id)+"?from=50&to=250", nil)
	if !cycles.Success {
		t.Fatalf("cycles failed: %s", cycles.Msg)
	}
	var cycleRows []model.NodeTrafficCycle
	if err := json.Unmarshal(cycles.Obj, &cycleRows); err != nil {
		t.Fatal(err)
	}
	if len(cycleRows) != 2 {
		t.Fatalf("cycles=%+v", cycleRows)
	}
	foundProvider := false
	for _, row := range cycleRows {
		if row.Source == "provider_import" && row.ProviderBillableBytes == 12*(1<<30) {
			foundProvider = true
		}
	}
	if !foundProvider {
		t.Fatalf("provider cycle missing from cycles=%+v", cycleRows)
	}

	summary := doBillingControllerRequest(t, engine, http.MethodGet, "/panel/api/billing/summary?asOf=150", nil)
	if !summary.Success {
		t.Fatalf("summary failed: %s", summary.Msg)
	}
	var summaryObj struct {
		ByCurrency map[string]struct {
			BaseMinor    int64 `json:"baseMinor"`
			OverageMinor int64 `json:"overageMinor"`
			TotalMinor   int64 `json:"totalMinor"`
		} `json:"byCurrency"`
	}
	if err := json.Unmarshal(summary.Obj, &summaryObj); err != nil {
		t.Fatal(err)
	}
	if got := summaryObj.ByCurrency["CNY"]; got.BaseMinor != 100 || got.OverageMinor != 4 || got.TotalMinor != 104 {
		t.Fatalf("summary=%+v", summaryObj)
	}
}

func itoa(v int) string {
	return strconv.Itoa(v)
}
