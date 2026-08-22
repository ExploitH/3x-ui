package singboxadapter

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadOnlyHandlerNormalizesTokenWhitespace(t *testing.T) {
	h, err := NewReadOnlyHandler(ReadOnlyOptions{
		ConfigJSON: []byte(`{"inbounds":[]}`),
		Token:      "  normalized-token  \n",
		BasePath:   "/adapter/",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/adapter/healthz", nil)
	req.Header.Set("Authorization", "Bearer normalized-token")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}
