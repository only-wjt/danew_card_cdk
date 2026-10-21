package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
	"github.com/danew/cdk-recharge-system/internal/db"
)

// Grok 批量充值：发码带 grok_ 前缀、本站存前缀档位；预检不产 preflight_token，兑换改带登录态。
func TestBatchRechargeGrokRedeemsWithCredential(t *testing.T) {
	openHandlerTestDB(t)
	const cookie = "sso=fixture-grok-sso-value; sso-rw=fixture"
	redeemed := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		switch r.URL.Path {
		case "/openapi/v1/gpt-direct/cdks":
			if b["plan"] != "grok_premium_monthly" || b["payment_country"] != "US" {
				t.Errorf("wrong Grok issue parameters %v", b)
			}
			w.Write([]byte(`{"code":0,"data":{"requested":1,"issued":[{"id":41,"code":"ZC-FAKE-GROK-BATCH-0001","plan":"premium_monthly"}]}}`))
		case "/api/v1/cdk/preview":
			w.Write([]byte(`{"redemption_token":"rt-grok","product":"grok","plan":"premium_monthly"}`))
		case "/api/v1/cdk/preflight":
			cred, _ := b["credential"].(map[string]any)
			if cred["session"] != cookie {
				t.Errorf("preflight credential %v", cred)
			}
			w.Write([]byte(`{"code":0,"data":{"email":"g@example.com","currentPlan":"free"}}`))
		case "/api/v1/cdk/redeem":
			redeemed++
			if _, has := b["preflight_token"]; has {
				t.Errorf("Grok redeem must not send preflight_token: %v", b)
			}
			cred, _ := b["credential"].(map[string]any)
			if cred["mode"] != "session" || cred["session"] != cookie {
				t.Errorf("Grok redeem credential %v", cred)
			}
			w.Write([]byte(`{"status":"completed","order_id":"o-1"}`))
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	if err := db.SetSetting("card_api_base", s.URL); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting("card_api_key", "fixture-key"); err != nil {
		t.Fatal(err)
	}
	job := &batchRechargeJob{seq: 1, clientRequestID: "brc-test-001", paymentCountry: "US",
		cred: batchRechargeCredential{Mode: "session", Session: cookie}}
	fatal, _ := submitOneRecharge(cardplatform.NewFromSettings(), "grok_premium_monthly", job)
	if fatal || !job.terminal || redeemed != 1 {
		t.Fatalf("fatal=%v terminal=%v redeemed=%d", fatal, job.terminal, redeemed)
	}
	if _, plan, _, _, ok := db.LookupStoredCDKByCode("ZC-FAKE-GROK-BATCH-0001"); !ok || plan != "grok_premium_monthly" {
		t.Fatalf("stored plan %q ok=%v", plan, ok)
	}
}

// Grok 账号被卡台拦截（blocking_reason）时不提交兑换。
func TestBatchRechargeGrokBlockingReasonStopsRedeem(t *testing.T) {
	openHandlerTestDB(t)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/cdk/preview":
			w.Write([]byte(`{"redemption_token":"rt-grok","product":"grok","plan":"premium_monthly"}`))
		case "/api/v1/cdk/preflight":
			w.Write([]byte(`{"code":0,"data":{"blocking_reason":"账号已有 Grok 订阅"}}`))
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	_ = db.SetSetting("card_api_base", s.URL)
	_ = db.SetSetting("card_api_key", "fixture-key")
	job := &batchRechargeJob{seq: 1, clientRequestID: "brc-test-002",
		cred: batchRechargeCredential{CDKCode: "ZC-FAKE-GROK-OWNED-0001", Mode: "session", Session: "sso=fixture"}}
	// 代理路径：码已在手，批次档位来自代理请求；以预览的 product 判 Grok
	if fatal, _ := submitOneRecharge(cardplatform.NewFromSettings(), "premium_monthly", job); fatal || !job.terminal {
		t.Fatalf("fatal=%v terminal=%v", fatal, job.terminal)
	}
}

func TestBatchCredentialProductMismatch(t *testing.T) {
	grok := batchRechargeCredential{Mode: "session", Session: "sso=abc"}
	gpt := batchRechargeCredential{Mode: "session", Session: `{"sessionToken":"x"}`}
	mail := batchRechargeCredential{Mode: "mailbox", Email: "a@b.c", Password: "p"}
	if batchCredentialProductMismatch("grok", grok) != "" || batchCredentialProductMismatch("gpt", gpt) != "" {
		t.Fatal("matching credential rejected")
	}
	if batchCredentialProductMismatch("grok", gpt) == "" || batchCredentialProductMismatch("grok", mail) == "" {
		t.Fatal("non-Grok credential accepted for Grok batch")
	}
	if batchCredentialProductMismatch("gpt", grok) == "" {
		t.Fatal("Grok cookie accepted for ChatGPT batch")
	}
}

func TestBatchPlanAllowedGrok(t *testing.T) {
	if !batchPlanAllowed("grok", "premium_monthly", map[string]bool{"premium_monthly": true}) {
		t.Fatal("sellable Grok plan rejected")
	}
	if !batchPlanAllowed("grok", "ultra_yearly", nil) || batchPlanAllowed("grok", "plus", nil) {
		t.Fatal("Grok fallback wrong")
	}
}
