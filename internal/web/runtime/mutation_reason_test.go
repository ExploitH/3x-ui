package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemotePropagatesQuotaMutationReasonHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		got = req.Header.Get(nodeMutationReasonHeader)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()
	r := NewRemote(nodeForPlainServer(t, srv, "verify", "tok"), nil)
	ctx := WithNodeMutationReason(context.Background(), MutationReasonQuotaBlock)
	if _, err := r.do(ctx, http.MethodPost, "panel/api/clients/update/alice?inboundIds=1", map[string]any{"enable": false}); err != nil {
		t.Fatalf("remote do: %v", err)
	}
	if got != MutationReasonQuotaBlock {
		t.Fatalf("mutation reason header=%q", got)
	}
}

func TestNodeMutationReasonDoesNotLeakIntoUnrelatedContext(t *testing.T) {
	if got := NodeMutationReason(context.Background()); got != "" {
		t.Fatalf("background mutation reason=%q", got)
	}
}
