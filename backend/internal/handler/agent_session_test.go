package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAgentCheckSessionRequired(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/check", AgentCheckSession)

	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest(http.MethodPost, "/check", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != false {
		t.Fatalf("ok = %v", out["ok"])
	}
	if out["error_code"] != "SESSION_REQUIRED" {
		t.Fatalf("error_code = %v", out["error_code"])
	}
}
