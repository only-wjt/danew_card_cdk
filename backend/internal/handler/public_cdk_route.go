package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/danew/cdk-recharge-system/internal/notify"
	"github.com/danew/cdk-recharge-system/internal/provider"
)

// siteRedeemRoute 从请求 body 认出本站码并解析到 preview 时选定的那台。
// isSite=false 时调用方按老路径直连主台，行为完全不变。
func siteRedeemRoute(body map[string]any) (route *provider.Route, siteCode string, isSite bool, err error) {
	code := str(body["code"])
	if code == "" {
		if found, ferr := db.FindCodeByRedemptionToken(str(body["redemption_token"])); ferr == nil {
			code = strings.TrimSpace(found)
		}
	}
	if !provider.IsSiteCode(code) {
		return nil, code, false, nil
	}
	r, rerr := provider.ResolveSticky(code)
	return r, code, true, rerr
}

// maskUpstreamCode 上游回包里可能回显它自己的码，直接透传等于把上游码泄给用户，
// 之后他就能绕过本站直接在卡台兑换。统一换回本站码。
func maskUpstreamCode(raw []byte, remoteCode, siteCode string) []byte {
	if len(raw) == 0 || remoteCode == "" || siteCode == "" {
		return raw
	}
	out := bytes.ReplaceAll(raw, []byte(remoteCode), []byte(siteCode))
	if len(remoteCode) > 14 {
		out = bytes.ReplaceAll(out, []byte(remoteCode[:14]), []byte(provider.SiteCodePrefixOf(siteCode)))
	}
	return out
}

func bindRedemptionTokens(siteCode string, raw []byte) {
	if tok := extractJSONString(raw, "redemption_token", "token"); tok != "" {
		_ = db.BindCDKRedemptionToken(siteCode, tok)
	}
	if tok := extractJSONNestedString(raw, "data", "redemption_token"); tok != "" {
		_ = db.BindCDKRedemptionToken(siteCode, tok)
	}
}

// sitePreview 本站码的 preview：这是唯一允许切台的一步。
// 此刻还没有任何上游会话和扣费，换台是安全的；选中后写死 active_binding。
func sitePreview(c *gin.Context, siteCode string) {
	route, st, raw, err := provider.PreviewWithFailover(c.Request.Context(), siteCode, deviceFrom(c))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if st >= 200 && st < 300 && route != nil {
		bindRedemptionTokens(siteCode, raw)
	}
	if route != nil {
		raw = maskUpstreamCode(raw, route.RemoteCode, siteCode)
	}
	proxyPublicJSON(c, st, raw)
}

// sitePreflight / siteRedeem 之后都不再切台：redemption_token 只在选定的那台有效。
func sitePreflight(c *gin.Context, route *provider.Route, siteCode string, body map[string]any) {
	body["code"] = route.RemoteCode
	st, raw, err := route.Provider.Preflight(c.Request.Context(), body, deviceFrom(c))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	raw = maskUpstreamCode(raw, route.RemoteCode, siteCode)
	tok := str(body["redemption_token"])
	if tok == "" {
		tok = extractJSONString(raw, "redemption_token", "token")
	}
	if sess := extractCredentialSession(body["credential"]); sess != "" {
		if berr := db.BindCDKSession(siteCode, tok, sess); berr != nil {
			log.Printf("[cdk-preflight] bind session failed code=%s: %v", siteCode, berr)
		}
	}
	proxyPublicJSON(c, st, raw)
}

func siteRedeem(c *gin.Context, route *provider.Route, siteCode string, body map[string]any) {
	claimed, cerr := db.ClaimBindingForRedeem(route.BindingID)
	if cerr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": cerr.Error()})
		return
	}
	if !claimed {
		c.JSON(http.StatusConflict, gin.H{"error": "该卡密已兑换或已失效"})
		return
	}
	body["code"] = route.RemoteCode
	st, raw, err := route.Provider.Redeem(c.Request.Context(), body, deviceFrom(c))
	if err != nil {
		// 请求都没打出去，码没被消耗，放回去让用户重试同一台。
		_ = db.UpdateBindingStatus(route.BindingID, db.BindingStatusUnused, err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	raw = maskUpstreamCode(raw, route.RemoteCode, siteCode)
	switch {
	case st >= 200 && st < 300:
		// 兑换成功：另一台那张必须立刻收回，否则同一本站码能兑两次。
		go provider.MarkConsumed(context.Background(), route)
	case st >= 400 && st < 500:
		// 业务拒绝（码无效/凭证不对），码没被消耗，允许改参数重试。
		_ = db.UpdateBindingStatus(route.BindingID, db.BindingStatusUnused, upstreamErrText(raw))
	default:
		// 5xx / 超时：上游可能已经扣了，状态未知。保持 redeeming 只允许同台重试，
		// 绝不能因此切到另一台，那会变成两台各扣一次。
		_ = db.UpdateBindingStatus(route.BindingID, db.BindingStatusRedeeming, upstreamErrText(raw))
	}
	proxyPublicJSON(c, st, raw)
}

func upstreamErrText(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		if len(raw) > 200 {
			return string(raw[:200])
		}
		return string(raw)
	}
	for _, k := range []string{"error", "message", "msg", "error_code"} {
		if s := str(m[k]); s != "" {
			return s
		}
	}
	return ""
}

// isTerminalRedeemSuccess result 回包是否表示已成功终态（异步兑换的成功点在这里）。
func isTerminalRedeemSuccess(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	cands := []any{payload["status"], payload["order_status"], payload["state"]}
	if data, ok := payload["data"].(map[string]any); ok {
		cands = append(cands, data["status"], data["order_status"], data["state"])
	}
	if order, ok := payload["order"].(map[string]any); ok {
		cands = append(cands, order["status"])
	}
	for _, v := range cands {
		switch strings.ToLower(str(v)) {
		case "completed", "success", "succeeded", "done":
			return true
		}
	}
	return false
}

func gptResultEmail(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if order, ok := payload["order"].(map[string]any); ok {
		if email := strAny(order["account_email"]); email != "" {
			return email
		}
	}
	return strAny(payload["account_email"])
}

func gptPlanLabel(plan string) string {
	switch strings.ToLower(strings.TrimSpace(plan)) {
	case "plus":
		return "Plus"
	case "pro_5x":
		return "Pro 5x"
	case "pro_20x":
		return "Pro"
	case "pro_25x":
		return "Pro 25x"
	case "pro_50x":
		return "Pro 50x"
	case "go":
		return "Go"
	case "credit250":
		return "Codex 点数 250"
	case "credit500":
		return "Codex 点数 500"
	case "credit1000":
		return "Codex 点数 1000"
	case "credit2500":
		return "Codex 点数 2500"
	case "credit5000":
		return "Codex 点数 5000"
	case "credit25000":
		return "Codex 点数 25000"
	default:
		if plan == "" {
			return "—"
		}
		return plan
	}
}

func notifyGPTSuccess(code, email string, payload map[string]any) {
	orderID := ""
	if payload != nil {
		orderID = strAny(payload["order_id"])
		if orderID == "" {
			orderID = strAny(payload["client_request_id"])
		}
		if email == "" {
			email = gptResultEmail(payload)
		}
	}
	key := orderID
	if key == "" {
		key = strings.ToUpper(strings.TrimSpace(code))
	}
	if key == "" || !noticeOnce("gpt:"+key) {
		return
	}
	plan := ""
	var country *string
	if code != "" {
		if p, c, ok := db.ClaimCDKSuccessNotice(code); ok {
			plan, country = p, c
		}
	}
	if plan == "" && payload != nil {
		plan = strAny(payload["plan"])
	}
	if country == nil && payload != nil {
		country = countryFromCurrency(strAny(payload["currency"]))
	}
	notify.ChatGPTRedeemed(gptPlanLabel(plan), email, notify.RegionLabel(country))
}

// ClaimNoticeOnce 同一笔开通只发一次。回调重试和结果轮询共用这把锁。
func noticeOnce(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	err := db.InsertWebhookEvent(0, "redeem_notice", "notice|"+key, "{}")
	return err == nil
}

func notifyChatGPTCompleted(payload map[string]any) {
	if payload == nil {
		return
	}
	orderID := strAny(payload["order_id"])
	if orderID == "" {
		orderID = strAny(payload["client_request_id"])
	}
	if orderID == "" || !noticeOnce("gpt:"+orderID) {
		return
	}
	email := strAny(payload["account_email"])
	region := notify.RegionLabel(countryFromCurrency(strAny(payload["currency"])))
	notify.ChatGPTRedeemed(gptPlanLabel(strAny(payload["plan"])), email, region)
}

func countryFromCurrency(currency string) *string {
	var code string
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "PHP":
		code = "PH"
	case "USD":
		code = "US"
	case "JPY":
		code = "JP"
	case "KRW":
		code = "KR"
	case "CLP":
		code = "CL"
	case "EGP":
		code = "EG"
	case "INR":
		code = "IN"
	case "NGN":
		code = "NG"
	case "TRY":
		code = "TR"
	default:
		return nil
	}
	return &code
}
