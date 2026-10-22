package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/danew/cdk-recharge-system/internal/db"
)

func (f *fakeCardPlatform) issueCalls() []fakeIssueCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeIssueCall(nil), f.calls...)
}

func (f *fakeCardPlatform) set(fn func(*fakeCardPlatform)) {
	f.mu.Lock()
	fn(f)
	f.mu.Unlock()
}

func TestCardnetOverviewListsGrok(t *testing.T) {
	e := newCardnetEnv(t)
	_, key := e.client(db.CardnetClientInput{
		AllowedPlans: []string{"plus", "grok_premium_monthly"}, AllowedCountries: []string{"PH"}, DailyCodeLimit: 9,
	})
	st, out := e.do(http.MethodGet, "/api/v1/cardnet/overview", key, "", "")
	if st != http.StatusOK || out["issuable"] != true {
		t.Fatalf("overview: %d %v", st, out)
	}
	// 可售 ∩ 授权：grok_plus_monthly 可售但未授权，不列。
	var got []string
	for _, p := range out["plans"].([]any) {
		m := p.(map[string]any)
		got = append(got, fmt.Sprintf("%s|%s|%s", m["product"], m["key"], m["label"]))
	}
	if fmt.Sprint(got) != "[gpt|plus|Plus grok|grok_premium_monthly|Grok Premium 月付]" {
		t.Fatalf("plans: %v", got)
	}
	if len(out["payment_regions"].([]any)) != 2 {
		t.Fatalf("top-level regions stay GPT: %v", out["payment_regions"])
	}
	products := out["products"].(map[string]any)
	if products["gpt"].(map[string]any)["available"] != true || products["grok"].(map[string]any)["fixed_payment_country"] != "US" {
		t.Fatalf("products: %v", products)
	}
}

func TestCardnetGrokCatalogueFailureKeepsGPT(t *testing.T) {
	e := newCardnetEnv(t)
	e.up.set(func(f *fakeCardPlatform) { f.grokMode = "fail" })
	_, key := e.client(db.CardnetClientInput{AllowedPlans: []string{"plus", "grok_premium_monthly"}, DailyCodeLimit: 9})
	st, out := e.do(http.MethodGet, "/api/v1/cardnet/overview", key, "", "")
	if st != http.StatusOK || out["issuable"] != true || len(out["plans"].([]any)) != 1 {
		t.Fatalf("GPT should stay sellable: %d %v", st, out)
	}
	grok := out["products"].(map[string]any)["grok"].(map[string]any)
	if grok["available"] != false || grok["message"] == nil {
		t.Fatalf("grok should be marked unavailable: %v", grok)
	}
	if st, out := e.issue(key, "g-plus", `{"plan":"plus","count":1}`); st != http.StatusOK {
		t.Fatalf("GPT issue: %d %v", st, out)
	}
}

func TestCardnetIssueGrok(t *testing.T) {
	e := newCardnetEnv(t)
	// 允许地区只勾了 PH：Grok 不受地区授权约束。
	client, key := e.client(db.CardnetClientInput{
		AllowedPlans: []string{"grok_premium_monthly", "plus"}, AllowedCountries: []string{"PH"}, DailyCodeLimit: 3,
	})
	st, out := e.issue(key, "dj-GK-1-0", `{"plan":"grok_premium_monthly","count":2,"external_order_no":"DJGK"}`)
	if st != http.StatusOK || out["plan"] != "grok_premium_monthly" || out["payment_country"] != "US" {
		t.Fatalf("grok issue: %d %v", st, out)
	}
	calls := e.up.issueCalls()
	if len(calls) != 1 || calls[0].Plan != "grok_premium_monthly" || calls[0].PaymentCountry != "US" {
		t.Fatalf("card platform should get the prefixed plan and US: %+v", calls)
	}
	codes := issuedCodes(t, out)
	for _, it := range codes {
		if it["plan"] != "grok_premium_monthly" || it["payment_country"] != "US" {
			t.Fatalf("issued: %v", it)
		}
		var plan, country string
		var clientID int64
		if err := db.DB.QueryRow(`SELECT plan, COALESCE(payment_country,''), cardnet_client_id FROM cardplatform_cdk_codes WHERE id = ?`,
			int64(it["id"].(float64))).Scan(&plan, &country, &clientID); err != nil {
			t.Fatal(err)
		}
		// 卡台回的是裸键，落库必须补前缀，否则查单会显示成 X。
		if plan != "grok_premium_monthly" || country != "US" || clientID != client.ID {
			t.Fatalf("stored: plan=%s country=%s client=%d", plan, country, clientID)
		}
		if res := lookupOneCDK(context.Background(), it["code"].(string), "", false); res.Status != "unused" {
			t.Fatalf("lookup: %+v", res)
		}
	}

	// 空地区与 US 是同一个请求：同键重放，不算换参。
	st, again := e.issue(key, "dj-GK-1-0", `{"plan":"GROK_premium_monthly","count":2,"payment_country":"us"}`)
	if st != http.StatusOK || again["replayed"] != true || fmt.Sprint(issuedCodes(t, again)) != fmt.Sprint(codes) {
		t.Fatalf("replay with explicit US: %d %v", st, again)
	}

	// 非 US 明确拒绝，不打卡台。
	st, out = e.issue(key, "dj-GK-2-0", `{"plan":"grok_premium_monthly","count":1,"payment_country":"PH"}`)
	if st != http.StatusUnprocessableEntity || out["error_code"] != "COUNTRY_UNSUPPORTED" {
		t.Fatalf("grok PH: %d %v", st, out)
	}

	// 旧键换新键后按新键授权；额度 GPT 与 Grok 共用（已用 2，剩 1）。
	if st, out := e.issue(key, "dj-GK-3-0", `{"plan":"grok_monthly","count":1}`); st != http.StatusOK || out["plan"] != "grok_premium_monthly" {
		t.Fatalf("legacy key: %d %v", st, out)
	}
	if st, out := e.issue(key, "dj-GK-4-0", `{"plan":"plus","count":1}`); st != http.StatusForbidden || out["error_code"] != "DAILY_LIMIT_EXCEEDED" {
		t.Fatalf("shared quota: %d %v", st, out)
	}
	if n := len(e.up.issueCalls()); n != 2 {
		t.Fatalf("upstream calls: %d", n)
	}
	// 管理端按产品筛请求。
	for product, want := range map[string]int{"grok": 2, "gpt": 0, "": 2} {
		if _, total, err := db.ListCardnetRequests(db.CardnetRequestQuery{Product: product}); err != nil || total != want {
			t.Fatalf("filter %q: total=%d err=%v", product, total, err)
		}
	}

	// 作废与产品无关：Grok 码同样删除退费。
	id := int64(codes[0]["id"].(float64))
	st, res := e.do(http.MethodPost, "/api/v1/cardnet/cdks/revoke", key, "", fmt.Sprintf(`{"ids":[%d]}`, id))
	if st != http.StatusOK || fmt.Sprint(res["revoked"]) != fmt.Sprintf("[%d]", id) {
		t.Fatalf("revoke grok: %d %v", st, res)
	}
}

func TestCardnetPlanKeyRules(t *testing.T) {
	e := newCardnetEnv(t)
	_, key := e.client(db.CardnetClientInput{
		AllowedPlans: []string{"plus", "grok_premium_monthly", "grok_ultra_yearly"}, DailyCodeLimit: 50,
	})
	cases := []struct {
		name, body, code, hint string
		status                 int
	}{
		{"bare premium is X", `{"plan":"premium_monthly","count":1}`, "PLAN_NOT_SELLABLE", "grok_premium_monthly", 422},
		{"x prefix", `{"plan":"x_basic_monthly","count":1}`, "PLAN_NOT_SELLABLE", "X", 422},
		{"bare grok", `{"plan":"plus_monthly","count":1}`, "PLAN_NOT_SELLABLE", "grok_plus_monthly", 422},
		{"unknown grok", `{"plan":"grok_mega_monthly","count":1}`, "PLAN_NOT_SELLABLE", "Grok", 422},
		{"grok disabled upstream", `{"plan":"grok_ultra_yearly","count":1}`, "PLAN_NOT_SELLABLE", "", 422},
		{"grok not allowed", `{"plan":"grok_plus_monthly","count":1}`, "PLAN_NOT_ALLOWED", "", 403},
	}
	for i, tc := range cases {
		st, out := e.issue(key, fmt.Sprintf("rule-%d", i), tc.body)
		if st != tc.status || out["error_code"] != tc.code || !strings.Contains(fmt.Sprint(out["message"]), tc.hint) {
			t.Errorf("%s: %d %v", tc.name, st, out)
		}
	}
	// 老卡台忽略 product 参数：Grok 目录为空，不能放行到卡台。
	resetCardnetUpstreamCache()
	e.up.set(func(f *fakeCardPlatform) { f.grokMode = "ignore" })
	if st, out := e.issue(key, "rule-old", `{"plan":"grok_premium_monthly","count":1}`); st != 422 || out["error_code"] != "PLAN_NOT_SELLABLE" {
		t.Errorf("old card platform: %d %v", st, out)
	}
	if n := len(e.up.keys()); n != 0 {
		t.Fatalf("rejected requests reached the card platform %d times", n)
	}
}

func TestCardnetGrokPlanMismatchNeedsReview(t *testing.T) {
	e := newCardnetEnv(t)
	_, key := e.client(db.CardnetClientInput{AllowedPlans: []string{"grok_premium_monthly"}, DailyCodeLimit: 5})
	e.up.set(func(f *fakeCardPlatform) { f.returnPlan = "premium_yearly" })
	st, out := e.issue(key, "dj-PM-1-0", `{"plan":"grok_premium_monthly","count":1}`)
	if st != http.StatusBadGateway || out["error_code"] != "UPSTREAM_PLAN_MISMATCH" || out["status"] != "review" {
		t.Fatalf("mismatch: %d %v", st, out)
	}
	var stored int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM cardplatform_cdk_codes WHERE issue_source='cardnet'`).Scan(&stored)
	if stored != 0 {
		t.Fatalf("mismatched codes must not be stored, got %d", stored)
	}
	if st, out := e.issue(key, "dj-PM-1-0", `{"plan":"grok_premium_monthly","count":1}`); st != http.StatusBadGateway || out["error_code"] != "UPSTREAM_PLAN_MISMATCH" {
		t.Fatalf("retry on review: %d %v", st, out)
	}
}

func TestAdminCardnetClientPlanValidation(t *testing.T) {
	e := newCardnetEnv(t)
	e.router.POST("/admin/cardnet/clients", AdminCreateCardnetClient)
	e.router.PUT("/admin/cardnet/clients/:id", AdminUpdateCardnetClient)

	for _, plan := range []string{"premium_monthly", "x_basic_monthly", "plus_monthly"} {
		body := fmt.Sprintf(`{"name":"bad","allowed_plans":["plus",%q]}`, plan)
		if st, out := e.do(http.MethodPost, "/admin/cardnet/clients", "", "", body); st != http.StatusBadRequest {
			t.Errorf("%s should be rejected: %d %v", plan, st, out)
		}
	}
	st, out := e.do(http.MethodPost, "/admin/cardnet/clients", "", "", `{"name":"ok","allowed_plans":["Plus","grok_lite_monthly","grok_premium_monthly"]}`)
	if st != http.StatusOK {
		t.Fatalf("create: %d %v", st, out)
	}
	client := out["client"].(map[string]any)
	if fmt.Sprint(client["allowed_plans"]) != "[plus grok_plus_monthly grok_premium_monthly]" {
		t.Fatalf("plans should be canonical: %v", client["allowed_plans"])
	}
	id := int64(client["id"].(float64))
	if st, _ := e.do(http.MethodPut, fmt.Sprintf("/admin/cardnet/clients/%d", id), "", "", `{"name":"ok","allowed_plans":["premium_yearly"]}`); st != http.StatusBadRequest {
		t.Fatalf("update with X key: %d", st)
	}

	// 历史上已存的不合规键：停用/启用回传整份授权时不能因此失败。
	gptTestSQL(t, `UPDATE cardnet_clients SET allowed_plans = 'plus,premium_monthly' WHERE id = ?`, id)
	st, out = e.do(http.MethodPut, fmt.Sprintf("/admin/cardnet/clients/%d", id), "", "",
		`{"name":"ok","allowed_plans":["plus","premium_monthly"],"status":"disabled"}`)
	if st != http.StatusOK || out["client"].(map[string]any)["status"] != "disabled" {
		t.Fatalf("toggle with legacy key: %d %v", st, out)
	}
}
