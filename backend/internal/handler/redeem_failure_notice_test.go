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
				for _, want := range []string{"ChatGPT 兑换失败", "Plus", code, "美区", "card &lt;declined&gt;"} {
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

	other := base
	other.ClientRequestID, other.UpstreamOrderID, other.Status = "brc-test-002", "", itemStatusSubmitted
	notifyBatchItemFailure(&other, itemStatusUnknown, "提交结果未知（网络异常）")
	stillUnknown := other
	stillUnknown.Status = itemStatusUnknown
	notifyBatchItemFailure(&stillUnknown, itemStatusUnknown, "提交结果未知（网络异常）")
	notifyBatchItemFailure(&other, itemStatusSuccess, "开通完成")
	gptQueueCount(t, 2)

	texts := failureNoticeTexts(t)
	if !strings.Contains(texts[0], "ChatGPT 兑换失败") || !strings.Contains(texts[0], "brc-test") || !strings.Contains(texts[0], "card declined") {
		t.Fatalf("batch failure notice: %s", texts[0])
	}
	if !strings.Contains(texts[1], "结果未知，请核对") {
		t.Fatalf("batch unknown notice: %s", texts[1])
	}
}
