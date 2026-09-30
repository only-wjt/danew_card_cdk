package cardplatform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGraceRecoveryUsesPublicScopedTokens(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/api/v1/cdk/recover-subscription" {
			t.Error("incorrect route")
		}
		if r.Header.Get("X-API-Key") != "" || r.Header.Get("Authorization") != "" {
			t.Error("privileged key leaked")
		}
		if r.Header.Get("X-Redemption-Device") != "synthetic-device" {
			t.Error("device binding missing")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["confirmed"] != true {
			t.Error("confirmation missing")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":0,"data":{"status":"pending"}}`))
	}))
	defer srv.Close()
	c := New(Config{SiteBase: srv.URL, APIKey: "never-send-this-key"})
	status, raw, err := c.RecoverSubscription(context.Background(), map[string]any{"redemption_token": "synthetic", "preflight_token": "synthetic", "confirmed": true}, "synthetic-device")
	if err != nil || status != 200 || len(raw) == 0 || calls != 1 {
		t.Fatalf("unexpected result status=%d calls=%d err=%v", status, calls, err)
	}
	if c.client.Timeout != defaultHTTPTimeout {
		t.Fatal("changed shared client timeout")
	}
}
