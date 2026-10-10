package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/danew/cdk-recharge-system/internal/provider"
	"github.com/gin-gonic/gin"
)

func gptTestPayload(t *testing.T, raw string) map[string]any {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGPTPayloadShapes(t *testing.T) {
	cases := []struct {
		raw     string
		success bool
	}{
		{`{"event":"gpt_direct.completed","order_id":"ord_1","account_email":"a@example.com","plan":"plus","currency":"USD"}`, true},
		{`{"type":"openai.direct.completed","id":"evt_1","data":{"object":{"orderId":"ord_1","email":"a@example.com","plan":"plus","currency":"USD"}}}`, true},
		{`{"type":"openai.direct.completed","data":{"object":{"order":{"id":"ord_1","status":"completed","account_email":"a@example.com","plan":"plus","currency":"USD"}}}}`, true},
		{`{"status":"success","data":{"order":{"id":"ord_1","state":"done","email":"a@example.com","plan":"plus","currency":"USD"}}}`, true},
		{`{"order":{"order_id":"ord_1","order_status":"succeeded","email":"a@example.com","plan":"plus","currency":"USD"}}`, true},
		{`{"event":"gpt_direct.completed","object":{"order_id":"ord_1","email":"a@example.com","plan":"plus","currency":"USD"}}`, true},
		{`{"id":"ord_1","status":"completed","email":"a@example.com","plan":"plus","currency":"USD"}`, true},
		{`{"type":"openai.direct.completed","status":"success","data":{"order":{"id":"ord_1","status":"pending","email":"a@example.com","plan":"plus","currency":"USD"}}}`, false},
		{`{"status":"success","data":{"object":{"order":{"orderId":"ord_1","status":"failed","email":"a@example.com","plan":"plus","currency":"USD"}}}}`, false},
		{`{"type":"openai.direct.completed","data":{"object":{"order_id":"ord_1","state":"processing","email":"a@example.com","plan":"plus","currency":"USD"}}}`, false},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			p := gptTestPayload(t, tc.raw)
			if got := isTerminalRedeemSuccess(p); got != tc.success {
				t.Fatalf("success=%v want %v", got, tc.success)
			}
			if got := gptOrderID(p); got != "ord_1" {
				t.Fatalf("id=%q", got)
			}
			if got := gptResultEmail(p); got != "a@example.com" {
				t.Fatalf("email=%q", got)
			}
			if got := gptPayloadField(p, "plan"); got != "plus" {
				t.Fatalf("plan=%q", got)
			}
			if got := gptPayloadField(p, "currency"); got != "USD" {
				t.Fatalf("currency=%q", got)
			}
		})
	}
	if id := gptOrderID(gptTestPayload(t, `{"type":"openai.direct.completed","id":"evt_1","client_request_id":"client_1"}`)); id != "" {
		t.Fatalf("event/client id used as order: %q", id)
	}
}

func gptTestSQL(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := db.DB.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func gptTestAccount(t *testing.T, id int64) {
	t.Helper()
	gptTestSQL(t, `INSERT INTO card_platform_accounts (id,name,site_base,protocol,cred_secret,webhook_secret,capabilities,status)
 VALUES (?,?,'https://invalid.example','cardplatform','test','gpt-test-secret','openai','active')`, id, fmt.Sprintf("test-%d", id))
}

func gptWebhookRequest(t *testing.T, id int64, raw string) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/hook/:accountId", CardPlatformWebhook)
	req := httptest.NewRequest("POST", fmt.Sprintf("/hook/%d", id), strings.NewReader(raw))
	mac := hmac.New(sha256.New, []byte("gpt-test-secret"))
	_, _ = mac.Write([]byte(raw))
	req.Header.Set("X-Webhook-Signature", hex.EncodeToString(mac.Sum(nil)))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func gptQueueCount(t *testing.T, want int) {
	t.Helper()
	var got int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM telegram_notifications`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("queue count=%d want=%d", got, want)
	}
}

func TestGPTWebhookPollingDedupAndAccountIsolation(t *testing.T) {
	openHandlerTestDB(t)
	gptTestAccount(t, 101)
	gptTestAccount(t, 102)
	// Old permanent notice rows must not suppress a new durable queue insert.
	if err := db.InsertWebhookEvent(0, "redeem_notice", "notice|gpt:ord_1", "{}"); err != nil {
		t.Fatal(err)
	}
	hook := `{"type":"openai.direct.completed","id":"evt_1","data":{"object":{"orderId":"ord_1","email":"a@example.com","plan":"plus","currency":"USD"}}}`
	for _, id := range []int64{101, 101, 102} {
		if status := gptWebhookRequest(t, id, hook); status != 200 {
			t.Fatalf("status=%d", status)
		}
	}
	polling := gptTestPayload(t, `{"status":"success","data":{"order":{"id":"ord_1","status":"completed","account_email":"a@example.com"}}}`)
	if err := notifyGPTSuccess(101, "", "", polling); err != nil {
		t.Fatal(err)
	}
	gptQueueCount(t, 2)
	var text string
	if err := db.DB.QueryRow(`SELECT text FROM telegram_notifications WHERE business_key=?`, gptNotificationKey(101, "ord_1")).Scan(&text); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"Plus", "a@example.com", "美区"} {
		if !strings.Contains(text, value) {
			t.Fatalf("missing %s in %s", value, text)
		}
	}
}

func TestGPTWebhookPersistenceAndEnqueueRetry(t *testing.T) {
	openHandlerTestDB(t)
	gptTestAccount(t, 101)
	hook := `{"type":"gpt_direct.completed","order_id":"ord_retry","account_email":"a@example.com"}`
	gptTestSQL(t, `CREATE TRIGGER fail_event BEFORE INSERT ON webhook_events BEGIN SELECT RAISE(FAIL,'unique-looking store failure'); END`)
	if status := gptWebhookRequest(t, 101, hook); status != 503 {
		t.Fatalf("store failure=%d", status)
	}
	gptQueueCount(t, 0)
	gptTestSQL(t, `DROP TRIGGER fail_event`)
	gptTestSQL(t, `CREATE TRIGGER fail_queue BEFORE INSERT ON telegram_notifications BEGIN SELECT RAISE(FAIL,'queue unavailable'); END`)
	if status := gptWebhookRequest(t, 101, hook); status != 503 {
		t.Fatalf("enqueue failure=%d", status)
	}
	var received int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM webhook_events WHERE event_type='gpt_direct.completed'`).Scan(&received); err != nil || received != 1 {
		t.Fatalf("received=%d err=%v", received, err)
	}
	gptTestSQL(t, `DROP TRIGGER fail_queue`)
	if status := gptWebhookRequest(t, 101, hook); status != 200 {
		t.Fatalf("duplicate retry=%d", status)
	}
	gptQueueCount(t, 1)
}

func TestGPTWebhookMalformedAndPending(t *testing.T) {
	openHandlerTestDB(t)
	gptTestAccount(t, 101)
	for _, raw := range []string{`{`, `null`, `[]`} {
		if status := gptWebhookRequest(t, 101, raw); status != 400 {
			t.Fatalf("malformed=%d", status)
		}
	}
	var count int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM webhook_events`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("recorded malformed count=%d err=%v", count, err)
	}
	for _, state := range []string{"pending", "failed"} {
		hook := fmt.Sprintf(`{"type":"openai.direct.completed","id":"evt_%s","status":"success","data":{"object":{"order":{"id":"ord_%s","status":"%s"}}}}`, state, state, state)
		if status := gptWebhookRequest(t, 101, hook); status != 200 {
			t.Fatalf("pending=%d", status)
		}
	}
	gptQueueCount(t, 0)
	if status := gptWebhookRequest(t, 101, `{"type":"gpt_direct.completed","id":"evt_only"}`); status != 503 {
		t.Fatalf("missing business identity=%d", status)
	}
	if status := gptWebhookRequest(t, 101, `{"type":"gpt_direct.completed","client_request_id":"legacy-client"}`); status != 200 {
		t.Fatalf("client fallback=%d", status)
	}
	gptQueueCount(t, 1)
}

func TestGPTMetadataClaimDoesNotLoseQueue(t *testing.T) {
	openHandlerTestDB(t)
	code := "GPT-NOTIFY-METADATA-TEST"
	if err := db.SaveCardplatformCDKCode(991, code, "GPT-NOTIFY", "plus", 100); err != nil {
		t.Fatal(err)
	}
	gptTestSQL(t, `UPDATE cardplatform_cdk_codes SET payment_country='JP' WHERE code=?`, code)
	payload := gptTestPayload(t, `{"order":{"id":"ord_meta","status":"completed"}}`)
	gptTestSQL(t, `CREATE TRIGGER fail_queue BEFORE INSERT ON telegram_notifications BEGIN SELECT RAISE(FAIL,'queue unavailable'); END`)
	if err := notifyGPTSuccess(101, code, "a@example.com", payload); err == nil {
		t.Fatal("expected queue failure")
	}
	var status string
	if err := db.DB.QueryRow(`SELECT status FROM cardplatform_cdk_codes WHERE code=?`, code).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status == "consumed" {
		t.Fatal("claimed before durable enqueue")
	}
	gptTestSQL(t, `DROP TRIGGER fail_queue`)
	// Simulate a historical metadata claim: queue still must be inserted.
	db.ClaimCDKSuccessNotice(code)
	if err := notifyGPTSuccess(101, code, "a@example.com", payload); err != nil {
		t.Fatal(err)
	}
	gptQueueCount(t, 1)
	var text string
	if err := db.DB.QueryRow(`SELECT text FROM telegram_notifications`).Scan(&text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Plus") || !strings.Contains(text, "日区") || !strings.Contains(text, code) {
		t.Fatalf("metadata text=%s", text)
	}
}

func TestGPTNoticeUsesSessionEmailAndCode(t *testing.T) {
	openHandlerTestDB(t)
	code := "GPT-SESSION-EMAIL"
	if err := db.SaveCardplatformCDKCode(101, code, "GPT", "plus", 100); err != nil {
		t.Fatal(err)
	}
	sess := `{"sessionToken":"st","user":{"email":"buyer@example.com"}}`
	if err := db.BindCDKSession(code, "tok", sess); err != nil {
		t.Fatal(err)
	}
	payload := gptTestPayload(t, `{"data":{"order":{"id":"ord_sess","status":"completed","plan":"plus"}}}`)
	if err := notifyGPTSuccess(101, code, "", payload); err != nil {
		t.Fatal(err)
	}
	var text string
	if err := db.DB.QueryRow(`SELECT text FROM telegram_notifications`).Scan(&text); err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"buyer@example.com", "卡密: <code>" + code + "</code>", "Plus"} {
		if !strings.Contains(text, part) {
			t.Fatalf("missing %s in %s", part, text)
		}
	}
}

func TestGPTPollingRetryErrors(t *testing.T) {
	openHandlerTestDB(t)
	gptTestAccount(t, 101)
	var raw atomic.Value
	raw.Store("{")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(raw.Load().(string)))
	}))
	defer upstream.Close()
	gptTestSQL(t, "UPDATE card_platform_accounts SET site_base=? WHERE id=101", upstream.URL)
	code := "GPT-POLL-RETRY"
	if err := db.SaveCardplatformCDKCode(992, code, "GPT-POLL", "plus", 100); err != nil {
		t.Fatal(err)
	}
	gptTestSQL(t, "UPDATE cardplatform_cdk_codes SET fulfilled_account_id=101 WHERE code=?", code)
	if err := db.BindCDKRedemptionToken(code, "poll-token"); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/result", PublicCDKResult)
	r.GET("/result-by-code", PublicCDKResultByCode)
	paths := []string{"/result?token=poll-token", "/result-by-code?code=" + code}
	request := func(path string) int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w.Code
	}
	for _, path := range paths {
		if status := request(path); status != 502 {
			t.Fatalf("parse %s status=%d", path, status)
		}
	}
	raw.Store(`{"status":"success","data":{"order":{"id":"ord_poll","status":"completed"}}}`)
	gptTestSQL(t, "CREATE TRIGGER fail_queue BEFORE INSERT ON telegram_notifications BEGIN SELECT RAISE(FAIL,'queue unavailable'); END")
	for _, path := range paths {
		if status := request(path); status != 503 {
			t.Fatalf("enqueue %s status=%d", path, status)
		}
	}
	gptTestSQL(t, "DROP TRIGGER fail_queue")
	for _, path := range paths {
		if status := request(path); status != 200 {
			t.Fatalf("retry %s status=%d", path, status)
		}
	}
	gptQueueCount(t, 1)
	if status := gptWebhookRequest(t, 101, `{"type":"openai.direct.completed","data":{"object":{"orderId":"ord_poll"}}}`); status != 200 {
		t.Fatalf("hook after poll=%d", status)
	}
	gptQueueCount(t, 1)
}

func TestGPTAccountResolution(t *testing.T) {
	openHandlerTestDB(t)
	if got, err := notificationAccountID(0, ""); err != nil || got != 0 {
		t.Fatalf("unknown=%d err=%v", got, err)
	}
	gptTestAccount(t, 101)
	gptTestAccount(t, 102)
	code := "GPT-ACCOUNT-TEST"
	if err := db.SaveCardplatformCDKCode(993, code, "GPT-ACCOUNT", "plus", 100); err != nil {
		t.Fatal(err)
	}
	gptTestSQL(t, "UPDATE cardplatform_cdk_codes SET fulfilled_account_id=102 WHERE code=?", code)
	if got, err := notificationAccountID(0, code); err != nil || got != 102 {
		t.Fatalf("legacy=%d err=%v", got, err)
	}
	if got, err := notificationAccountID(101, code); err != nil || got != 101 {
		t.Fatalf("explicit=%d err=%v", got, err)
	}
	if got, err := notificationAccountID(0, "DN-UNKNOWN"); err == nil {
		t.Fatalf("known site fell back=%d", got)
	}
	gptTestSQL(t, "DROP TABLE cardplatform_cdk_codes")
	if got, err := notificationAccountID(0, code); err == nil {
		t.Fatalf("store failure fell back=%d", got)
	}
}

func TestGPTImmediateRedeem(t *testing.T) {
	for _, site := range []bool{false, true} {
		for _, tc := range []struct {
			name, raw string
			status    int
			queued    bool
			failQueue bool
		}{
			{"completed", `{"data":{"order":{"id":"ord_immediate","status":"completed"}}}`, 200, true, false},
			{"nested-success", `{"status":"success","data":{"order":{"id":"ord_immediate","status":"success"}}}`, 200, true, false},
			{"nested-succeeded", `{"data":{"object":{"order":{"id":"ord_immediate","state":"succeeded"}}}}`, 200, true, false},
			{"nested-done", `{"order":{"id":"ord_immediate","order_status":"done"}}`, 200, true, false},
			{"object-success", `{"data":{"object":{"orderId":"ord_immediate","status":"success"}}}`, 200, true, false},
			{"envelope-success", `{"status":"success","data":{"order":{"id":"ord_immediate"}}}`, 200, false, false},
			{"envelope-succeeded", `{"status":"succeeded"}`, 200, false, false},
			{"envelope-done", `{"status":"done"}`, 200, false, false},
			{"nested-pending", `{"status":"success","data":{"object":{"order":{"id":"ord_immediate","status":"pending"}}}}`, 200, false, false},
			{"client-fallback", `{"status":"completed","client_request_id":"client_immediate"}`, 200, true, false},
			{"code-fallback", `{"status":"completed"}`, 200, true, false},
			{"pending", `{"status":"success","data":{"order":{"id":"ord_immediate","status":"pending"}}}`, 200, false, false},
			{"accepted", `{"status":"success"}`, 202, false, false},
			{"failed", `{"data":{"order":{"id":"ord_immediate","status":"failed"}}}`, 200, false, false},
			{"queue-failure", `{"data":{"order":{"id":"ord_immediate","status":"completed"}}}`, 200, false, true},
		} {
			t.Run(fmt.Sprintf("site=%v/%s", site, tc.name), func(t *testing.T) {
				openHandlerTestDB(t)
				gptTestAccount(t, 101)
				var recharges atomic.Int64
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == "POST" {
						recharges.Add(1)
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.raw))
				}))
				defer upstream.Close()
				gptTestSQL(t, "UPDATE card_platform_accounts SET site_base=?,protocol=? WHERE id=101", upstream.URL, provider.ProtocolSpaceXLegacy)
				acc, err := db.GetCardPlatformAccount(101)
				if err != nil {
					t.Fatal(err)
				}
				code := "GPT-IMMEDIATE"
				var bindingID int64
				var route *provider.Route
				if site {
					code = "DN-IMMEDIATE"
					row, err := db.CreatePendingSiteCDK(code, "plus", false, 100)
					if err != nil {
						t.Fatal(err)
					}
					bindingID, err = db.InsertSiteCDKBinding(db.SiteCDKBinding{SiteCodeID: row.ID, SiteCode: code, AccountID: 101, Provider: provider.ProtocolSpaceXLegacy, RemoteID: "900", RemoteCode: "GPT-REMOTE", IsPrimary: true, Status: db.BindingStatusUnused})
					if err != nil {
						t.Fatal(err)
					}
					// Exercise siteRedeem's claimed-binding path with a mocked upstream.
					// Sibling retirement is outside this test; disabling IsSite on this test
					// route keeps that unrelated background work out of the fixture lifecycle.
					route = &provider.Route{Account: acc, BindingID: bindingID, SiteCodeID: row.ID, RemoteCode: "GPT-REMOTE", Provider: provider.NewSpaceXLegacy(acc)}
				} else {
					if err := db.SaveCardplatformCDKCode(995, code, "GPT-IMMEDIATE", "plus", 100); err != nil {
						t.Fatal(err)
					}
					gptTestSQL(t, "UPDATE cardplatform_cdk_codes SET fulfilled_account_id=101 WHERE code=?", code)
				}
				if err := db.BindCDKRedemptionToken(code, "immediate-token"); err != nil {
					t.Fatal(err)
				}
				if tc.failQueue {
					gptTestSQL(t, "CREATE TRIGGER fail_queue BEFORE INSERT ON telegram_notifications BEGIN SELECT RAISE(FAIL,'queue unavailable'); END")
				}
				gin.SetMode(gin.TestMode)
				r := gin.New()
				if site {
					r.POST("/redeem", func(c *gin.Context) {
						var body map[string]any
						if err := c.ShouldBindJSON(&body); err != nil {
							t.Fatal(err)
						}
						siteRedeem(c, route, code, body)
					})
				} else {
					r.POST("/redeem", PublicCDKRedeem)
				}
				r.GET("/result", PublicCDKResult)
				w := httptest.NewRecorder()
				clientID := "client_immediate"
				if tc.name == "code-fallback" {
					clientID = ""
				}
				req := httptest.NewRequest("POST", "/redeem", strings.NewReader(fmt.Sprintf(`{"code":%q,"redemption_token":"immediate-token","client_request_id":%q}`, code, clientID)))
				req.Header.Set("Content-Type", "application/json")
				r.ServeHTTP(w, req)
				want := tc.status
				if tc.failQueue {
					want = 503
				}
				if w.Code != want {
					t.Fatalf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
				}
				count := 0
				if tc.queued {
					count = 1
				}
				gptQueueCount(t, count)
				if tc.failQueue {
					response := gptTestPayload(t, w.Body.String())
					if response["retry_action"] != "result" || response["redeem_accepted"] != true || isTerminalRedeemSuccess(response) {
						t.Fatalf("unsafe retry response: %s", w.Body.String())
					}
					if site {
						var status string
						if err := db.DB.QueryRow("SELECT status FROM site_cdk_bindings WHERE id=?", bindingID).Scan(&status); err != nil {
							t.Fatal(err)
						}
						if status == db.BindingStatusUnused || status == db.BindingStatusFailed {
							t.Fatalf("released accepted binding: %s", status)
						}
					}
					gptTestSQL(t, "DROP TRIGGER fail_queue")
					// Recover via a read-only observation, not by resubmitting the recharge.
					if err := notifyGPTSuccess(101, code, "", gptTestPayload(t, tc.raw)); err != nil {
						t.Fatal(err)
					}
					gptQueueCount(t, 1)
				}
				if tc.queued || tc.failQueue {
					hook := fmt.Sprintf(`{"type":"openai.direct.completed","data":{"object":{"orderId":"ord_immediate","clientRequestId":%q,"code":%q}}}`, clientID, code)
					if got := gptWebhookRequest(t, 101, hook); got != 200 {
						t.Fatalf("hook=%d", got)
					}
					if err := notifyGPTSuccess(101, code, "", gptTestPayload(t, tc.raw)); err != nil {
						t.Fatal(err)
					}
					gptQueueCount(t, 1)
				}
				if recharges.Load() != 1 {
					t.Fatalf("recharges=%d", recharges.Load())
				}
			})
		}
	}
}

func TestGPTFallbackAliasConvergence(t *testing.T) {
	for _, fallback := range []string{"client", "code", "site-remote-code"} {
		for _, sent := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/sent=%v", fallback, sent), func(t *testing.T) {
				openHandlerTestDB(t)
				gptTestAccount(t, 101)
				gptTestAccount(t, 102)
				code := "GPT-ALIAS"
				if fallback == "site-remote-code" {
					code = "DN-ALIAS"
				}
				if err := db.SaveCardplatformCDKCode(996, code, "ALIAS", "plus", 100); err != nil {
					t.Fatal(err)
				}
				echoedCode := code
				if fallback == "site-remote-code" {
					echoedCode = "GPT-REMOTE-ALIAS"
					row, ok := db.GetSiteCDKByCode(code)
					if !ok {
						t.Fatal("missing code")
					}
					if _, err := db.InsertSiteCDKBinding(db.SiteCDKBinding{SiteCodeID: row.ID, SiteCode: code, AccountID: 101, Provider: provider.ProtocolSpaceXLegacy, RemoteID: "901", RemoteCode: echoedCode, IsPrimary: true, Status: db.BindingStatusUnused}); err != nil {
						t.Fatal(err)
					}
				}
				first := `{"type":"gpt_direct.completed","id":"evt_fallback","client_request_id":"client_alias"}`
				if fallback == "client" {
					if got := gptWebhookRequest(t, 101, first); got != 200 {
						t.Fatalf("client-only webhook=%d", got)
					}
				} else {
					// Code-only polling notification, with case/whitespace normalization.
					if err := notifyGPTSuccess(101, " "+strings.ToLower(code)+" ", "", gptTestPayload(t, `{"status":"completed"}`)); err != nil {
						t.Fatal(err)
					}
				}
				gptQueueCount(t, 1)
				var originalText string
				if err := db.DB.QueryRow("SELECT text FROM telegram_notifications").Scan(&originalText); err != nil {
					t.Fatal(err)
				}
				if sent {
					gptTestSQL(t, "UPDATE telegram_notifications SET status='sent',sent_at=1")
				}
				rich := fmt.Sprintf(`{"type":"openai.direct.completed","id":"evt_richer","data":{"object":{"order":{"id":"ord_alias","status":"completed","clientRequestId":"client_alias","code":%q}}}}`, echoedCode)
				if got := gptWebhookRequest(t, 101, rich); got != 200 {
					t.Fatalf("richer webhook=%d", got)
				}
				if err := notifyGPTSuccess(101, code, "", gptTestPayload(t, `{"data":{"order":{"id":"ord_alias","status":"completed","client_request_id":"client_alias"}}}`)); err != nil {
					t.Fatal(err)
				}
				canonicalOnly := `{"type":"openai.direct.completed","data":{"object":{"orderId":"ord_alias"}}}`
				if got := gptWebhookRequest(t, 101, canonicalOnly); got != 200 {
					t.Fatalf("canonical-only webhook=%d", got)
				}
				gptQueueCount(t, 1)
				var text, status string
				if err := db.DB.QueryRow("SELECT text,status FROM telegram_notifications").Scan(&text, &status); err != nil {
					t.Fatal(err)
				}
				if text != originalText || (sent && status != "sent") {
					t.Fatalf("existing delivery changed text=%s status=%s", text, status)
				}
				// The same order/client/code identifiers in another account are distinct.
				if got := gptWebhookRequest(t, 102, rich); got != 200 {
					t.Fatalf("other account=%d", got)
				}
				gptQueueCount(t, 2)
			})
		}
	}
}

func TestGPTAliasStorageFailureIsRetryable(t *testing.T) {
	openHandlerTestDB(t)
	gptTestAccount(t, 101)
	gptTestSQL(t, "CREATE TRIGGER fail_alias BEFORE INSERT ON telegram_notification_aliases BEGIN SELECT RAISE(FAIL,'alias unavailable'); END")
	hook := `{"type":"gpt_direct.completed","id":"evt_alias_retry","client_request_id":"client_retry"}`
	if got := gptWebhookRequest(t, 101, hook); got != 503 {
		t.Fatalf("alias failure=%d", got)
	}
	gptQueueCount(t, 0)
	gptTestSQL(t, "DROP TRIGGER fail_alias")
	if got := gptWebhookRequest(t, 101, hook); got != 200 {
		t.Fatalf("duplicate retry=%d", got)
	}
	gptQueueCount(t, 1)
}

func TestGPTNotificationKeyPriority(t *testing.T) {
	p := gptTestPayload(t, `{"type":"gpt_direct.completed","id":"evt_envelope","order_id":"ord_priority","clientRequestId":"client_priority"}`)
	keys := gptNotificationKeys(101, " code-secret ", p)
	if len(keys) != 3 || keys[0] != gptNotificationKey(101, "ord_priority") || keys[1] != "gpt:account:101:client:client_priority" || !strings.HasPrefix(keys[2], "gpt:account:101:code:") {
		t.Fatalf("keys=%v", keys)
	}
	if strings.Contains(strings.Join(keys, "|"), "evt_envelope") || strings.Contains(keys[2], "CODE-SECRET") {
		t.Fatalf("unsafe key=%v", keys)
	}
	if got := gptNotificationKeys(101, "", gptTestPayload(t, `{"type":"gpt_direct.completed","id":"evt_only"}`)); len(got) != 0 {
		t.Fatalf("event treated as identity=%v", got)
	}
}

func TestGPTAvanfinityEffectiveClientAlias(t *testing.T) {
	for _, echo := range []bool{false, true} {
		t.Run(fmt.Sprintf("echo=%v", echo), func(t *testing.T) {
			openHandlerTestDB(t)
			gptTestAccount(t, 101)
			original := "web-abcd-1720000000000"
			effective := provider.AvanfinityRedeemClientRequestID(original)
			var wireID atomic.Value
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				wireID.Store(strAny(body["clientRequestId"]))
				w.Header().Set("Content-Type", "application/json")
				if echo {
					_, _ = fmt.Fprintf(w, `{"data":{"order":{"status":"succeeded","clientRequestId":%q}}}`, effective)
				} else {
					_, _ = w.Write([]byte(`{"data":{"order":{"status":"success"}}}`))
				}
			}))
			defer upstream.Close()
			gptTestSQL(t, "UPDATE card_platform_accounts SET protocol=?,site_base=? WHERE id=101", provider.ProtocolAvanfinity202608, upstream.URL)
			acc, err := db.GetCardPlatformAccount(101)
			if err != nil {
				t.Fatal(err)
			}
			row, err := db.CreatePendingSiteCDK("DN-AVAN-ALIAS", "plus", false, 100)
			if err != nil {
				t.Fatal(err)
			}
			bindingID, err := db.InsertSiteCDKBinding(db.SiteCDKBinding{SiteCodeID: row.ID, SiteCode: row.Code, AccountID: 101, Provider: provider.ProtocolAvanfinity202608, RemoteID: "avan-1", RemoteCode: "AVF-TEST", IsPrimary: true, Status: db.BindingStatusUnused})
			if err != nil {
				t.Fatal(err)
			}
			route := &provider.Route{Account: acc, BindingID: bindingID, SiteCodeID: row.ID, RemoteCode: "AVF-TEST", Provider: provider.NewAvanfinityV2026(acc)}
			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.POST("/redeem", func(c *gin.Context) {
				var body map[string]any
				if err := c.ShouldBindJSON(&body); err != nil {
					t.Fatal(err)
				}
				siteRedeem(c, route, row.Code, body)
			})
			req := httptest.NewRequest("POST", "/redeem", strings.NewReader(fmt.Sprintf(`{"redemption_token":"rt","preflight_token":"pf","client_request_id":%q}`, original)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != 200 {
				t.Fatalf("redeem=%d body=%s", w.Code, w.Body.String())
			}
			if wireID.Load() != effective {
				t.Fatalf("wire=%v expected=%s", wireID.Load(), effective)
			}
			gptQueueCount(t, 1)
			for _, client := range []string{original, effective} {
				var count int
				if err := db.DB.QueryRow("SELECT COUNT(*) FROM telegram_notification_aliases WHERE alias_key=?", fmt.Sprintf("gpt:account:101:client:%s", client)).Scan(&count); err != nil || count != 1 {
					t.Fatalf("alias %s count=%d err=%v", client, count, err)
				}
			}
			// The callback has only canonical order and the transformed UUID, no code.
			hook := fmt.Sprintf(`{"type":"openai.direct.completed","data":{"object":{"orderId":"ord_avan_alias","clientRequestId":%q}}}`, effective)
			if got := gptWebhookRequest(t, 101, hook); got != 200 {
				t.Fatalf("callback=%d", got)
			}
			if got := gptWebhookRequest(t, 101, `{"type":"openai.direct.completed","data":{"object":{"orderId":"ord_avan_alias"}}}`); got != 200 {
				t.Fatalf("canonical-only callback=%d", got)
			}
			if err := notifyGPTSuccess(101, row.Code, "", gptTestPayload(t, `{"data":{"order":{"id":"ord_avan_alias","status":"done"}}}`)); err != nil {
				t.Fatal(err)
			}
			gptQueueCount(t, 1)
		})
	}
}
