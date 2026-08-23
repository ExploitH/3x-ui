package singboxadapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

func managedAPIRequest(h http.Handler, method, target string, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer adapter-test-token")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	return resp
}

func TestManagedClientCRUDEndpointsMatchRemotePayloads(t *testing.T) {
	mutator, configPath, statePath := writeMutatorFixture(t)
	h, err := NewReadOnlyHandler(ReadOnlyOptions{
		ConfigPath:          configPath,
		Token:               "adapter-test-token",
		BasePath:            "/adapter/",
		ManagedRelayMutator: mutator,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	state, err := NewManagedRelayStateStore(statePath).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.Itoa(stableInboundID(state.InboundTag))

	addBody := `{"client":{"email":"bob@example.com","auth":"bob-secret","enable":true},"inboundIds":[` + id + `]}`
	if resp := managedAPIRequest(h, http.MethodPost, "/adapter/panel/api/clients/add", addBody, nil); resp.Code != http.StatusOK {
		t.Fatalf("add status=%d body=%s", resp.Code, resp.Body.String())
	}
	state, err = NewManagedRelayStateStore(statePath).Load(context.Background())
	if err != nil || len(state.Users) != 2 {
		t.Fatalf("after add state=%+v err=%v", state, err)
	}

	updateBody := `{"email":"bob@example.com","auth":"bob-updated","enable":false}`
	if resp := managedAPIRequest(h, http.MethodPost, "/adapter/panel/api/clients/update/bob%40example.com?inboundIds="+id, updateBody, nil); resp.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", resp.Code, resp.Body.String())
	}
	state, err = NewManagedRelayStateStore(statePath).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var bob ManagedRelayStateUser
	for _, user := range state.Users {
		if user.Email == "bob@example.com" {
			bob = user
		}
	}
	if bob.Password != "bob-updated" || bob.Enabled || bob.AdminEnabled {
		t.Fatalf("after update bob=%+v", bob)
	}

	detachBody := `{"inboundIds":[` + id + `]}`
	if resp := managedAPIRequest(h, http.MethodPost, "/adapter/panel/api/clients/bob%40example.com/detach", detachBody, nil); resp.Code != http.StatusOK {
		t.Fatalf("detach status=%d body=%s", resp.Code, resp.Body.String())
	}
	state, err = NewManagedRelayStateStore(statePath).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range state.Users {
		if user.Email == "bob@example.com" && (!user.Detached || user.Enabled) {
			t.Fatalf("after detach bob=%+v", user)
		}
	}

	if resp := managedAPIRequest(h, http.MethodPost, "/adapter/panel/api/clients/del/bob%40example.com", "", nil); resp.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", resp.Code, resp.Body.String())
	}
	state, err = NewManagedRelayStateStore(statePath).Load(context.Background())
	if err != nil || len(state.Users) != 1 || state.Users[0].Email != "alice@example.com" {
		t.Fatalf("after delete state=%+v err=%v", state, err)
	}
	config, err := os.ReadFile(configPath)
	if err != nil || strings.Contains(string(config), "bob@example.com") {
		t.Fatalf("deleted config=%q err=%v", config, err)
	}
}

func TestManagedClientUpdateReasonSeparatesQuotaBlockFromAdminState(t *testing.T) {
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
	state, err := NewManagedRelayStateStore(statePath).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.Itoa(stableInboundID(state.InboundTag))
	target := "/adapter/panel/api/clients/update/alice%40example.com?inboundIds=" + id
	body := `{"email":"alice@example.com","enable":false}`
	if resp := managedAPIRequest(h, http.MethodPost, target, body, map[string]string{"X-3x-UI-Mutation-Reason": "quota-block"}); resp.Code != http.StatusOK {
		t.Fatalf("quota block status=%d body=%s", resp.Code, resp.Body.String())
	}
	state, err = NewManagedRelayStateStore(statePath).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Users[0].Enabled || !state.Users[0].AdminEnabled || !state.Users[0].QuotaBlocked {
		t.Fatalf("quota block state=%+v", state.Users[0])
	}
	if resp := managedAPIRequest(h, http.MethodPost, target, `{"email":"alice@example.com","enable":true}`, map[string]string{"X-3x-UI-Mutation-Reason": "quota-reset"}); resp.Code != http.StatusOK {
		t.Fatalf("quota reset status=%d body=%s", resp.Code, resp.Body.String())
	}
	state, err = NewManagedRelayStateStore(statePath).Load(context.Background())
	if err != nil || !state.Users[0].Enabled || state.Users[0].QuotaBlocked {
		t.Fatalf("quota reset state=%+v err=%v", state.Users[0], err)
	}
}

func TestManagedClientAddRejectsMissingOrWrongInboundScope(t *testing.T) {
	mutator, _, _ := writeMutatorFixture(t)
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
	body := `{"client":{"email":"bob@example.com","auth":"bob-secret","enable":true}}`
	for _, payload := range []string{body, `{"client":{"email":"bob@example.com","auth":"bob-secret","enable":true},"inboundIds":[999]}`} {
		resp := managedAPIRequest(h, http.MethodPost, "/adapter/panel/api/clients/add", payload, nil)
		if resp.Code != http.StatusBadRequest {
			t.Fatalf("payload=%s status=%d body=%s", payload, resp.Code, resp.Body.String())
		}
	}
}

func TestManagedRelayClientPayloadMarshalShape(t *testing.T) {
	payload := ManagedRelayClient{Email: "alice@example.com", Auth: "secret", Enable: true}
	encoded, err := json.Marshal(payload)
	if err != nil || !strings.Contains(string(encoded), `"auth":"secret"`) {
		t.Fatalf("encoded=%s err=%v", encoded, err)
	}
}
