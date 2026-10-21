package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/danew/cdk-recharge-system/internal/db"
)

func failureNoticeTexts(t *testing.T) []string {
	t.Helper()
	rows, err := db.DB.Query(`SELECT text FROM telegram_notifications ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// 卡台拒绝兑换（4xx）发失败通知；409（已兑换/兑换中）是去查原订单，不算失败。
func TestRedeemRejectedQueuesFailureNotice(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   int
	}{{"rejected", 400, 1}, {"conflict", 409, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			openHandlerTestDB(t)
			gptTestAccount(t, 101)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":"card <declined>"}`))
			}))
			defer upstream.Close()
			gptTestSQL(t, "UPDATE card_platform_accounts SET site_base=?,protocol='spacexcard-legacy' WHERE id=101", upstream.URL)
			code := "GPT-REJECT-0001"
			if err := db.SaveCardplatformCDKCode(996, code, "GPT-REJECT", "plus", 100, "US"); err != nil {
				t.Fatal(err)
			}
			gptTestSQL(t, "UPDATE cardplatform_cdk_codes SET fulfilled_account_id=101 WHERE code=?", code)
			if err := db.BindCDKRedemptionToken(code, "reject-token"); err != nil {
				t.Fatal(err)
			}
			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.POST("/redeem", PublicCDKRedeem)
			for i := 0; i < 2; i++ { // 同一请求号重放只发一条
				w := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/redeem", strings.NewReader(`{"code":"`+code+`","redemption_token":"reject-token","client_request_id":"web-reject-1"}`))
				req.Header.Set("Content-Type", "application/json")
				r.ServeHTTP(w, req)
				if w.Code != tc.status {
					t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
				}
			}
			gptQueueCount(t, tc.want)
			if tc.want == 1 {
				text := failureNoticeTexts(t)[0]
				// 失败后码常放回可用，通知里打码：前 10 位 + 后 4 位
				for _, want := range []string{"ChatGPT 兑换失败", "Plus", "GPT-REJECT…0001", "美区", "card &lt;declined&gt;"} {
					if !strings.Contains(text, want) {
						t.Fatalf("notice missing %q: %s", want, text)
					}
				}
			}
		})
	}
}

// 轮询拿到失败终态：同一订单反复轮询只发一条；Grok 码标题是 Grok。
func TestResultFailureNoticeOncePerOrder(t *testing.T) {
	openHandlerTestDB(t)
	gptTestAccount(t, 101)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"order":{"id":"ord_fail_1","status":"declined","decline_reason":"insufficient funds","plan":"premium_monthly"}}}`))
	}))
	defer upstream.Close()
	gptTestSQL(t, "UPDATE card_platform_accounts SET site_base=?,protocol='spacexcard-legacy' WHERE id=101", upstream.URL)
	code := "GROK-FAIL-0001"
	if err := db.SaveCardplatformCDKCode(997, code, "GROK-FAIL", "grok_premium_monthly", 100, "US"); err != nil {
		t.Fatal(err)
	}
	gptTestSQL(t, "UPDATE cardplatform_cdk_codes SET fulfilled_account_id=101 WHERE code=?", code)
	if err := db.BindCDKRedemptionToken(code, "fail-token"); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/result", PublicCDKResult)
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/result?token=fail-token", nil))
		if w.Code != 200 {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}
	gptQueueCount(t, 1)
	text := failureNoticeTexts(t)[0]
	for _, want := range []string{"Grok 兑换失败", "Grok Premium 月付", code, "insufficient funds"} {
		if !strings.Contains(text, want) {
			t.Fatalf("notice missing %q: %s", want, text)
		}
	}
}

// 批量充值：只有已提交兑换的明细失败/未知才通知，同一结局只发一次。
func TestBatchItemFailureNotice(t *testing.T) {
	openHandlerTestDB(t)
	base := db.AdminRechargeItem{BatchID: "brc-test", ClientRequestID: "brc-test-001", Plan: "plus",
		AccountEmail: "a@example.com", CDKCode: "GPT-BATCH-0001", UpstreamOrderID: "ord_b1"}

	pending := base
	pending.Status = itemStatusPreparing
	notifyBatchItemFailure(&pending, itemStatusFailed, "账号预检失败：session expired")
	gptQueueCount(t, 0)

	submitted := base
	submitted.Status = itemStatusSubmitted
	notifyBatchItemFailure(&submitted, itemStatusFailed, "提交被拒绝：card declined")
	notifyBatchItemFailure(&submitted, itemStatusFailed, "提交被拒绝：card declined")
	gptQueueCount(t, 1)

	// 提交时网络抖动落 unknown 不通知（几秒后通常就查到结果了）；只有轮询超时才发「结果未知」
	other := base
	other.ClientRequestID, other.UpstreamOrderID, other.Status = "brc-test-002", "", itemStatusSubmitted
	notifyBatchItemFailure(&other, itemStatusUnknown, "提交结果未知（网络异常）")
	notifyBatchItemFailure(&other, itemStatusSuccess, "开通完成")
	gptQueueCount(t, 1)
	enqueueBatchItemNotice(&other, itemStatusUnknown, "结果未知，请核对", "上游长时间未返回终态")
	enqueueBatchItemNotice(&other, itemStatusUnknown, "结果未知，请核对", "上游长时间未返回终态")
	gptQueueCount(t, 2)

	texts := failureNoticeTexts(t)
	if !strings.Contains(texts[0], "ChatGPT 兑换失败") || !strings.Contains(texts[0], "brc-test") || !strings.Contains(texts[0], "card declined") {
		t.Fatalf("batch failure notice: %s", texts[0])
	}
	if !strings.Contains(texts[1], "结果未知，请核对") {
		t.Fatalf("batch unknown notice: %s", texts[1])
	}
}

// 用户改了就会重试的拒绝（频率限制、会话/预检票失效、已有套餐）不通知；余额不足照常通知。
func TestRedeemRejectedSkipsUserRetryErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		skip   bool
	}{
		{429, `{"error":"slow down"}`, true},
		{400, `{"error_code":"GPT_SESSION_INVALID","error":"session invalid"}`, true},
		{400, `{"error_code":"PREFLIGHT_TOKEN_EXPIRED"}`, true},
		{400, `{"error_code":"GPT_PLAN_ALREADY_ACTIVE"}`, true},
		{400, `{"error_code":"INSUFFICIENT_BALANCE","error":"余额不足"}`, false},
		{400, `{"error_code":"GPT_DIRECT_ORDER_REJECTED"}`, false},
	}
	for _, tc := range cases {
		if got := gptUserRetryRejection(tc.status, gptTestPayload(t, tc.body)); got != tc.skip {
			t.Fatalf("%d %s: skip=%v want %v", tc.status, tc.body, got, tc.skip)
		}
	}
}

// 卡台失败回调：用户关了页面也能收到；与轮询同一个订单只发一条。
func TestWebhookFailedEventQueuesNoticeOnce(t *testing.T) {
	openHandlerTestDB(t)
	gptTestAccount(t, 101)
	hook := `{"type":"gpt_direct.failed","data":{"object":{"orderId":"ord_hook_fail","status":"failed","failure_reason":"card declined","plan":"plus"}}}`
	for i := 0; i < 2; i++ {
		if got := gptWebhookRequest(t, 101, hook); got != 200 {
			t.Fatalf("hook=%d", got)
		}
	}
	gptQueueCount(t, 1)
	if err := notifyGPTFailure(101, "", gptTestPayload(t, `{"data":{"order":{"id":"ord_hook_fail","status":"failed"}}}`), ""); err != nil {
		t.Fatal(err)
	}
	gptQueueCount(t, 1)
	if text := failureNoticeTexts(t)[0]; !strings.Contains(text, "card declined") {
		t.Fatalf("webhook notice: %s", text)
	}
}

func TestFailureNoticeLabelsAndStaleOrders(t *testing.T) {
	if p, _ := cardProductPlan("premium_monthly", nil); p != "X" {
		t.Fatalf("bare premium_* should be X, got %s", p)
	}
	if p, plan := cardProductPlan("premium_monthly", map[string]any{"product": "grok"}); p != "Grok" || plan != "grok_premium_monthly" {
		t.Fatalf("grok product: %s %s", p, plan)
	}
	if got := gptPlanLabel("premium_plus_yearly"); got != "X Premium+ 年付" {
		t.Fatalf("x label %q", got)
	}
	if got := gptFailureReason(gptTestPayload(t, `{"message":"success","data":{"order":{"status":"declined"}}}`)); got != "" {
		t.Fatalf("generic envelope message used as reason: %q", got)
	}
	if !staleFailure(map[string]any{"updated_at": "2020-01-01T00:00:00Z"}) || staleFailure(map[string]any{}) {
		t.Fatal("stale detection wrong")
	}
}
