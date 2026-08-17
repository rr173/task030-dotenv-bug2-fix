package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeNullJSONRejected(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/parse", bytes.NewBufferString("null"))
	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("null JSON status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
