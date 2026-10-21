package cardplatform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGrokCatalogueIsProductScopedAndUSOnly(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openapi/v1/gpt-direct/plans" || r.URL.Query().Get("product") != "grok" {
			t.Errorf("wrong product request %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":0,"data":{"version":1,"payment_regions":[{"country":"PH","currency":"PHP"},{"country":"US","currency":"USD"}],"plans":{"grok_premium_monthly":{"enabled":true,"currency":"USD","serviceFeeUsdMinor":20},"grok_ultra_yearly":{"enabled":false,"currency":"USD"}},"registry":[{"key":"premium_monthly","label":"Grok Premium 月付"},{"key":"ultra_yearly","label":"Grok Ultra 年付"},{"key":"plus_monthly","label":"Grok Plus 月付"}]}}`))
	}))
	defer s.Close()
	plans, err := New(Config{SiteBase: s.URL, APIKey: "fixture-key"}).GetPlans(context.Background(), "grok")
	if err != nil {
		t.Fatal(err)
	}
	rows := plans.SellablePlans()
	// ultra_yearly 定价停用、plus_monthly 定价表里没有：都不能卖。
	// premium_monthly 裸键也是 X 的键，但这里是 Grok 目录，必须照样能卖。
	if len(rows) != 1 || rows[0].Key != "premium_monthly" || rows[0].ServiceFeeUsdMinor != 20 {
		t.Fatalf("bad Grok catalogue %+v", rows)
	}
	if len(plans.PaymentRegions) != 1 || plans.PaymentRegions[0].Country != "US" {
		t.Fatalf("Grok must be US/USD only, got %+v", plans.PaymentRegions)
	}
}

func TestGrokPlansAndCredentials(t *testing.T) {
	for _, p := range []string{"plus_monthly", "super_yearly", "ultra_monthly", "grok_premium_monthly", "grok_premium_yearly",
		"monthly", "yearly", "lite_monthly", "heavy_monthly", "grok_heavy_yearly"} { // 后五个是改名前的旧键
		if !IsGrokPlan(p) {
			t.Fatal(p)
		}
	}
	for _, p := range []string{"plus", "pro_20x", "premium_monthly", "premium_plus_monthly", "pro_monthly", "pro_plus_monthly"} {
		if IsGrokPlan(p) {
			t.Fatal("cross product plan", p)
		}
	}
	if GrokIssuePlan("premium_monthly") != "grok_premium_monthly" || GrokIssuePlan("grok_super_yearly") != "grok_super_yearly" {
		t.Fatal("issue plan prefix")
	}
	// 旧键发码时换成新键；★plus_* 不做旧义兼容★（它现在就是 $8 的 Grok Plus）
	for in, want := range map[string]string{"monthly": "grok_premium_monthly", "grok_lite_yearly": "grok_plus_yearly",
		"heavy_monthly": "grok_ultra_monthly", "plus_monthly": "grok_plus_monthly"} {
		if got := GrokIssuePlan(in); got != want {
			t.Fatalf("GrokIssuePlan(%q)=%q want %q", in, got, want)
		}
	}
	if !IsGrokCredential("sso=abc.def") || !IsGrokCredential("sso-rw=x; sso=y") || IsGrokCredential(`{"sessionToken":"x"}`) || IsGrokCredential("abc") {
		t.Fatal("grok credential detection")
	}
}

func TestGrokPlanLabel(t *testing.T) {
	cases := map[string]string{
		"grok_premium_monthly": "Grok Premium 月付", "GROK_ultra_yearly": "Grok Ultra 年付",
		"grok_monthly": "Grok Premium 月付", "grok_heavy_yearly": "Grok Ultra 年付",
		"premium_monthly": "", "plus": "", "": "",
	}
	for in, want := range cases {
		if got := GrokPlanLabel(in); got != want {
			t.Fatalf("GrokPlanLabel(%q)=%q want %q", in, got, want)
		}
	}
}

func TestNormalizeListedPlan(t *testing.T) {
	cases := map[string]string{
		"super_monthly": "grok_super_monthly", "plus_yearly": "grok_plus_yearly", "ultra_monthly": "grok_ultra_monthly",
		"heavy_yearly": "grok_ultra_yearly", "monthly": "grok_premium_monthly", "lite_monthly": "grok_plus_monthly",
		// 与 X 同名、判不了的保持原样；GPT 档与已带前缀的不动
		"premium_monthly": "premium_monthly", "premium_plus_yearly": "premium_plus_yearly", "basic_monthly": "basic_monthly",
		"plus": "plus", "pro_20x_renew": "pro_20x_renew", "grok_premium_yearly": "grok_premium_yearly", "": "",
	}
	for in, want := range cases {
		if got := NormalizeListedPlan(in); got != want {
			t.Fatalf("NormalizeListedPlan(%q)=%q want %q", in, got, want)
		}
	}
}
