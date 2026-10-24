package cardplatform

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestCardnetCanonicalPlanGrokPromo(t *testing.T) {
	ok := map[string]string{
		"grok_super_monthly_promo":   "grok_super_monthly_promo",
		"GROK_Ultra_Monthly_PROMO":   "grok_ultra_monthly_promo",
		"grok_monthly_promo":         "grok_premium_monthly_promo", // 旧键换新键
		"grok_heavy_monthly_promo":   "grok_ultra_monthly_promo",
		"grok_premium_monthly_promo": "grok_premium_monthly_promo",
	}
	for in, want := range ok {
		got, product, err := CardnetCanonicalPlan(in)
		if err != nil || got != want || product != CardnetProductGrok {
			t.Errorf("%s → %s %s %v, want %s", in, got, product, err, want)
		}
	}
	for _, in := range []string{"grok_plus_monthly_promo", "grok_super_yearly_promo", "grok_mega_monthly_promo", "grok__promo"} {
		if _, _, err := CardnetCanonicalPlan(in); err == nil {
			t.Errorf("%s should be rejected", in)
		}
	}
}

func TestGrokPromoKeyHelpers(t *testing.T) {
	if !IsGrokPromoKey("grok_super_monthly_promo") || IsGrokPromoKey("grok_super_monthly") || IsGrokPromoKey("plus_promo") {
		t.Fatal("IsGrokPromoKey")
	}
	if got := GrokPlanLabel("grok_super_monthly_promo"); got != "Grok Super 月付（首月半价）" {
		t.Fatalf("label: %q", got)
	}
	if got := GrokPlanLabel("grok_plus_yearly_promo"); got != "" {
		t.Fatalf("no promo on plus yearly: %q", got)
	}
}

// 卡台 Grok 预检真实返回（节选）：plus 原价，premium / super / ultra 月付首月五折。
const grokPreflightSample = `{"code":0,"data":{"email":"a@example.com","currentPlan":"","payment_country":"US",
"quotes":{
 "plus_monthly":{"currency":"USD","plan":"plus_monthly","amountMinor":800,"listAmountMinor":800},
 "premium_monthly":{"currency":"USD","plan":"premium_monthly","amountMinor":1500,"listAmountMinor":3000,"discounted":true},
 "super_monthly":{"currency":"USD","plan":"super_monthly","amountMinor":5000,"listAmountMinor":10000,"discounted":true},
 "super_yearly":{"currency":"USD","plan":"super_yearly","amountMinor":100000,"listAmountMinor":100000}},
"promotions":{
 "plus_monthly":{"kind":"first_month_discount","eligible":false,"amount_minor":800,"list_amount_minor":800},
 "premium_monthly":{"kind":"first_month_discount","eligible":true,"discount_percent":50,"amount_minor":1500,"list_amount_minor":3000},
 "super_monthly":{"kind":"first_month_discount","eligible":true,"discount_percent":50,"amount_minor":5000,"list_amount_minor":10000}}},
"msg":"ok"}`

func TestParseGrokPromoQuote(t *testing.T) {
	raw := json.RawMessage(grokPreflightSample)
	cases := map[string]bool{
		"grok_super_monthly": true, "premium_monthly": true, "grok_plus_monthly": false, "super_yearly": false,
	}
	for plan, half := range cases {
		q, err := ParseGrokPromoQuote(raw, plan)
		if err != nil || q.HalfPrice() != half {
			t.Errorf("%s: %+v %v, want half=%v", plan, q, err, half)
		}
	}
	if q, _ := ParseGrokPromoQuote(raw, "super_monthly"); q.AmountMinor != 5000 || q.ListAmountMinor != 10000 || q.Currency != "USD" {
		t.Fatalf("super quote: %+v", q)
	}
	if _, err := ParseGrokPromoQuote(raw, "ultra_monthly"); !errors.Is(err, ErrGrokQuoteMissing) {
		t.Fatalf("missing plan: %v", err)
	}
	if _, err := ParseGrokPromoQuote(json.RawMessage(`{"code":0,"data":{}}`), "super_monthly"); !errors.Is(err, ErrGrokQuoteMissing) {
		t.Fatalf("no quotes: %v", err)
	}

	// 报价打了折，优惠信息却说没资格：两边对不上按非半价处理。
	conflict := json.RawMessage(`{"quotes":{"super_monthly":{"amountMinor":5000,"listAmountMinor":10000}},
		"promotions":{"super_monthly":{"eligible":false}}}`)
	if q, err := ParseGrokPromoQuote(conflict, "super_monthly"); err != nil || q.HalfPrice() {
		t.Fatalf("conflict should not count as half price: %+v %v", q, err)
	}
	// 金额是字符串、档位用旧键、别的档位字段类型异常，都不影响这一档。
	loose := json.RawMessage(`{"code":0,"data":{"quotes":{"heavy_monthly":{"amountMinor":"10000","listAmountMinor":"20000"},
		"plus_monthly":"weird"},"promotions":[]}}`)
	if q, err := ParseGrokPromoQuote(loose, "grok_ultra_monthly"); err != nil || !q.HalfPrice() || q.PromoKnown {
		t.Fatalf("loose quote: %+v %v", q, err)
	}
	// 首月免费也算半价以内。
	if q, err := ParseGrokPromoQuote(json.RawMessage(`{"quotes":{"super_monthly":{"amountMinor":0,"listAmountMinor":10000}}}`), "super_monthly"); err != nil || !q.HalfPrice() {
		t.Fatalf("free first month: %+v %v", q, err)
	}
	// 不带金额的优惠档次：premium / super 月付半价，plus 与年付没有优惠。
	offers := GrokFirstMonthOffers(raw)
	if offers["premium_monthly"] != GrokOfferHalf || offers["super_monthly"] != GrokOfferHalf ||
		offers["plus_monthly"] != GrokOfferNone || offers["super_yearly"] != GrokOfferNone {
		t.Fatalf("offers: %v", offers)
	}
	// 只打七折不算半价。
	partial := json.RawMessage(`{"data":{"quotes":{"super_monthly":{"amountMinor":7000,"listAmountMinor":10000}}}}`)
	if q, err := ParseGrokPromoQuote(partial, "super_monthly"); err != nil || q.HalfPrice() {
		t.Fatalf("30%% off is not half price: %+v %v", q, err)
	}
}
