package handler

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/danew/cdk-recharge-system/internal/db"
)

// premium 月付的 Grok 预检报价：half=true 是首月五折的账号，false 是原价账号。
func grokPremiumPrices(half bool) map[string]any {
	amount := 3000
	if half {
		amount = 1500
	}
	return map[string]any{
		"quotes": map[string]any{
			"premium_monthly": map[string]any{"currency": "USD", "amountMinor": amount, "listAmountMinor": 3000, "discounted": half},
		},
		"promotions": map[string]any{
			"premium_monthly": map[string]any{"kind": "first_month_discount", "eligible": half, "amount_minor": amount, "list_amount_minor": 3000},
		},
	}
}

func (f *fakeCardPlatform) takePublicCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.publicCalls
	f.publicCalls = nil
	return out
}

func newGrokPromoEnv(t *testing.T) (*cardnetEnv, string) {
	t.Helper()
	e := newCardnetEnv(t)
	e.router.POST("/api/v1/public/cdk/preflight", PublicCDKPreflight)
	e.router.POST("/api/v1/public/cdk/redeem", PublicCDKRedeem)
	_, key := e.client(db.CardnetClientInput{
		AllowedPlans: []string{"grok_premium_monthly", "grok_premium_monthly_promo"}, DailyCodeLimit: 20,
	})
	return e, key
}

func (e *cardnetEnv) issueOne(key, idem, plan string) string {
	e.t.Helper()
	st, out := e.issue(key, idem, fmt.Sprintf(`{"plan":%q,"count":1}`, plan))
	if st != http.StatusOK {
		e.t.Fatalf("issue %s: %d %v", plan, st, out)
	}
	return issuedCodes(e.t, out)[0]["code"].(string)
}

func TestCardnetIssueGrokPromo(t *testing.T) {
	e, key := newGrokPromoEnv(t)

	st, ov := e.do(http.MethodGet, "/api/v1/cardnet/overview", key, "", "")
	if st != http.StatusOK {
		t.Fatalf("overview: %d %v", st, ov)
	}
	var rows []string
	for _, p := range ov["plans"].([]any) {
		m := p.(map[string]any)
		rows = append(rows, fmt.Sprintf("%s|%s", m["key"], m["label"]))
	}
	if fmt.Sprint(rows) != "[grok_premium_monthly|Grok Premium 月付 grok_premium_monthly_promo|Grok Premium 月付（首月半价）]" {
		t.Fatalf("overview rows: %v", rows)
	}

	st, out := e.issue(key, "dj-PROMO-1", `{"plan":"GROK_premium_monthly_promo","count":2}`)
	if st != http.StatusOK || out["plan"] != "grok_premium_monthly_promo" || out["payment_country"] != "US" {
		t.Fatalf("promo issue: %d %v", st, out)
	}
	// 卡台没有半价档位：按正价键发码。
	if calls := e.up.issueCalls(); len(calls) != 1 || calls[0].Plan != "grok_premium_monthly" {
		t.Fatalf("card platform should get the full-price key: %+v", calls)
	}
	for _, it := range issuedCodes(t, out) {
		if it["plan"] != "grok_premium_monthly_promo" {
			t.Fatalf("issued plan should echo the promo key: %v", it)
		}
		var plan string
		_ = db.DB.QueryRow(`SELECT plan FROM cardplatform_cdk_codes WHERE id = ?`, int64(it["id"].(float64))).Scan(&plan)
		if plan != "grok_premium_monthly" {
			t.Fatalf("stored plan should be the full-price key, got %s", plan)
		}
		codePlan, reqPlan, err := db.CardnetCodeRequestPlan(strings.ToLower(it["code"].(string)))
		if err != nil || codePlan != "grok_premium_monthly" || reqPlan != "grok_premium_monthly_promo" {
			t.Fatalf("promo lookup: %s %s %v", codePlan, reqPlan, err)
		}
	}
	// 半价与正价是不同参数：同键换档位算换参。
	if st, out := e.issue(key, "dj-PROMO-1", `{"plan":"grok_premium_monthly","count":2}`); st != http.StatusUnprocessableEntity || out["error_code"] != "IDEMPOTENCY_KEY_REUSED" {
		t.Fatalf("promo vs full on same key: %d %v", st, out)
	}
	// 没有首月半价的档位不开放 _promo。
	for _, plan := range []string{"grok_plus_monthly_promo", "grok_premium_yearly_promo"} {
		if st, out := e.issue(key, "dj-PROMO-"+plan, fmt.Sprintf(`{"plan":%q,"count":1}`, plan)); st != http.StatusUnprocessableEntity || out["error_code"] != "PLAN_NOT_SELLABLE" {
			t.Errorf("%s: %d %v", plan, st, out)
		}
	}
	// 授权按半价键单独给：只授权正价的客户端发不了半价。
	_, fullOnly := e.client(db.CardnetClientInput{Name: "full", AllowedPlans: []string{"grok_premium_monthly"}, DailyCodeLimit: 5})
	if st, out := e.issue(fullOnly, "dj-PROMO-2", `{"plan":"grok_premium_monthly_promo","count":1}`); st != http.StatusForbidden || out["error_code"] != "PLAN_NOT_ALLOWED" {
		t.Fatalf("promo without grant: %d %v", st, out)
	}
}

const grokTestCred = `{"mode":"session","session":"sso=abc; sso-rw=def"}`

// preview 走本站预览拿 token（前端的真实流程）。
func (e *cardnetEnv) preview(code string) string {
	e.t.Helper()
	st, out := e.do(http.MethodPost, "/api/v1/public/cdk/preview", "", "", fmt.Sprintf(`{"code":%q}`, code))
	tok, _ := out["data"].(map[string]any)["redemption_token"].(string)
	if st != http.StatusOK || tok == "" {
		e.t.Fatalf("preview %s: %d %v", code, st, out)
	}
	return tok
}

func (e *cardnetEnv) grokPreflight(code, tok string) (int, map[string]any) {
	return e.do(http.MethodPost, "/api/v1/public/cdk/preflight", "", "",
		fmt.Sprintf(`{"code":%q,"redemption_token":%q,"credential":%s}`, code, tok, grokTestCred))
}

// grokRedeem 与前端一致：只带 token + 登录态。extra 是额外拼进 body 的字段。
func (e *cardnetEnv) grokRedeem(tok, extra string) (int, map[string]any) {
	return e.do(http.MethodPost, "/api/v1/public/cdk/redeem", "", "",
		fmt.Sprintf(`{"redemption_token":%q,"credential":%s,"client_request_id":"t-1"%s}`, tok, grokTestCred, extra))
}

func TestGrokPromoCodeRedeemGate(t *testing.T) {
	e, key := newGrokPromoEnv(t)
	e.router.POST("/api/v1/public/cdk/preview", PublicCDKPreview)
	promo := e.issueOne(key, "dj-GATE-1", "grok_premium_monthly_promo")
	full := e.issueOne(key, "dj-GATE-2", "grok_premium_monthly")
	promoTok, fullTok := e.preview(promo), e.preview(full)

	// 原价账号：半价码在预检就被拦下。
	e.up.set(func(f *fakeCardPlatform) { f.grokPrices = grokPremiumPrices(false) })
	st, out := e.grokPreflight(promo, promoTok)
	if st != http.StatusUnprocessableEntity || out["error_code"] != grokPromoErrorCode ||
		!strings.Contains(fmt.Sprint(out["error"]), "没有首月半价资格") || strings.Contains(fmt.Sprint(out["error"]), "30.00") {
		t.Fatalf("promo code on full-price account: %d %v", st, out)
	}
	// 跳过预检直接兑换：服务端复核，卡台 /redeem 不会被调用。
	e.up.takePublicCalls()
	if st, out := e.grokRedeem(promoTok, ""); st != http.StatusUnprocessableEntity || out["error_code"] != grokPromoErrorCode {
		t.Fatalf("direct redeem must be rechecked: %d %v", st, out)
	}
	if calls := e.up.takePublicCalls(); fmt.Sprint(calls) != "[/preflight]" {
		t.Fatalf("blocked redeem reached the card platform: %v", calls)
	}

	// 改挂 token：拿正价码 + 半价码的 token 预检，cdk_session_bindings 会把这个 token 记到正价码名下；
	// 兑换仍按预览记录认出是半价码。（改挂发生在一次通过的预检里，比如先用半价账号预检，再换原价账号兑换。）
	e.up.set(func(f *fakeCardPlatform) { f.grokPrices = grokPremiumPrices(true) })
	if st, out := e.grokPreflight(full, promoTok); st != http.StatusOK {
		t.Fatalf("remap preflight: %d %v", st, out)
	}
	e.up.set(func(f *fakeCardPlatform) { f.grokPrices = grokPremiumPrices(false) })
	if b, _ := db.GetBindingByCDK(full); b == nil || b.RedemptionToken != promoTok {
		t.Fatalf("precondition: full code binding should now carry the promo token, got %+v", b)
	}
	if st, out := e.grokRedeem(promoTok, fmt.Sprintf(`,"code":%q`, full)); st != http.StatusUnprocessableEntity || out["error_code"] != grokPromoErrorCode {
		t.Fatalf("remapped token must still be treated as promo: %d %v", st, out)
	}
	// 本站没发过的 token（直接去卡台预览拿的）不放行。
	e.up.takePublicCalls()
	if st, out := e.grokRedeem("rt-from-card-platform", ""); st != http.StatusUnprocessableEntity || out["error_code"] != grokTokenErrorCode {
		t.Fatalf("unknown token: %d %v", st, out)
	}
	if calls := e.up.takePublicCalls(); len(calls) != 0 {
		t.Fatalf("unknown token reached the card platform: %v", calls)
	}

	// 正价码不做价格校验：原价账号、半价账号都照常兑换；别名字段不会转给卡台。
	for _, half := range []bool{false, true} {
		e.up.set(func(f *fakeCardPlatform) { f.grokPrices = grokPremiumPrices(half); f.publicBodies = nil })
		e.up.takePublicCalls()
		if st, out := e.grokRedeem(fullTok, fmt.Sprintf(`,"redemptionToken":%q`, promoTok)); st != http.StatusAccepted {
			t.Fatalf("full code redeem half=%v: %d %v", half, st, out)
		}
		if calls := e.up.takePublicCalls(); fmt.Sprint(calls) != "[/redeem]" {
			t.Fatalf("full code must not be rechecked: %v", calls)
		}
		e.up.mu.Lock()
		sent := e.up.publicBodies[len(e.up.publicBodies)-1]
		e.up.mu.Unlock()
		if _, ok := sent["redemptionToken"]; ok || sent["redemption_token"] != fullTok {
			t.Fatalf("redeem body forwarded to card platform: %v", sent)
		}
	}

	// 半价账号：半价码预检通过，兑换前复核一次再提交。
	e.up.set(func(f *fakeCardPlatform) { f.grokPrices = grokPremiumPrices(true) })
	// 通过时带 grok_promo 标记（兑换页显示「首月半价卡密」）；正价码不带。
	// 报价金额不回浏览器，只给不带金额的 first_month_offers，账号等字段照旧。
	st, out = e.grokPreflight(promo, promoTok)
	data, _ := out["data"].(map[string]any)
	if st != http.StatusOK || out["grok_promo"] != true || data["email"] != "a@example.com" ||
		data["quotes"] != nil || data["promotions"] != nil ||
		fmt.Sprint(data["first_month_offers"]) != "map[premium_monthly:half]" {
		t.Fatalf("promo code on half-price account: %d %v", st, out)
	}
	if st, out := e.grokPreflight(full, fullTok); st != http.StatusOK || out["grok_promo"] != nil {
		t.Fatalf("full code must not be flagged: %d %v", st, out)
	}
	e.up.takePublicCalls()
	if st, out := e.grokRedeem(promoTok, ""); st != http.StatusAccepted {
		t.Fatalf("promo redeem: %d %v", st, out)
	}
	if calls := e.up.takePublicCalls(); fmt.Sprint(calls) != "[/preflight /redeem]" {
		t.Fatalf("promo redeem should recheck then submit: %v", calls)
	}

	// 复核时卡台说码已被兑换（超时后重试）：原样回 409，前端去查单，而不是判失败。
	e.up.set(func(f *fakeCardPlatform) { f.preflightReject = http.StatusConflict })
	if st, out := e.grokRedeem(promoTok, ""); st != http.StatusConflict {
		t.Fatalf("upstream 409 should pass through: %d %v", st, out)
	}
	// 预检没给报价：确认不了价格就不兑。
	e.up.set(func(f *fakeCardPlatform) { f.preflightReject = 0; f.grokPrices = nil })
	if st, out := e.grokRedeem(promoTok, ""); st != http.StatusUnprocessableEntity || out["error"] != grokPromoUnknownMsg {
		t.Fatalf("missing quote: %d %v", st, out)
	}
}
