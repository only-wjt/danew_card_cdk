package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
	"github.com/danew/cdk-recharge-system/internal/db"
)

// 卡网 Grok 首月半价码（发码请求的档位带 _promo）：兑换前核对账号的首月报价，不是半价就拦下。
// 拦截都在提交卡台之前，卡密不消耗。正价码、管理端发的码、存量码都不做价格校验。

const (
	grokPromoErrorCode     = "GROK_PROMO_INELIGIBLE"
	grokPreflightErrorCode = "GROK_PREFLIGHT_REJECTED"
	grokTokenErrorCode     = "GROK_TOKEN_UNKNOWN"
)

const grokPromoUnknownMsg = "暂时无法确认这个 Grok 账号的首月价格，卡密没有被消耗，请稍后重试。"

// grokRedeemBodyKeys Grok 兑换转给卡台的字段。其余一律丢掉：卡台若认 redemptionToken / token 之类的别名，
// 客户端就能拿一个本站没校验过的 token 兑换。
var grokRedeemBodyKeys = map[string]bool{
	"redemption_token": true, "code": true, "credential": true, "preflight_token": true, "client_request_id": true, "clientRequestId": true,
	"no_auto_card_switch": true, "strict_card_preference": true, "exclude_card_ids": true,
}

// grokPromoPlan 任一个码是卡网 Grok 半价码时，返回它的正价档位。
func grokPromoPlan(codes ...string) (string, bool) {
	for _, code := range codes {
		if code == "" {
			continue
		}
		codePlan, reqPlan, err := db.CardnetCodeRequestPlan(code)
		if err != nil {
			if !errors.Is(err, db.ErrCardnetNotFound) {
				log.Printf("[grok-promo] lookup %s: %v", shortTok(code), err)
			}
			continue
		}
		if cardplatform.IsGrokPromoKey(reqPlan) {
			base, _ := cardplatform.SplitGrokPromo(reqPlan)
			return nonEmptyStr(codePlan, base), true
		}
	}
	return "", false
}

// isGrokPromoRedeem 这次兑换的码（body 里的 code 或本站预览记录的 token）是不是半价码。
func isGrokPromoRedeem(body map[string]any) bool {
	previewCode, _ := db.PreviewTokenCode(str(body["redemption_token"]))
	_, promo := grokPromoPlan(str(body["code"]), previewCode)
	return promo
}

// grokRedeemGate Grok 兑换提交卡台前的把关；返回 false 时已写响应（一律 4xx：明确没提交卡台）。
//
// 卡台兑换认的是 redemption_token，所以 token 是哪张码只信本站预览时的记录（cdk_preview_tokens）：
// cdk_session_bindings 会被预检改写，客户端可以把半价码的 token 挂到正价码名下。
// 认不出来的 token 一律要求重新预览——Grok 兑换都经本站预览。
func grokRedeemGate(c *gin.Context, cli *cardplatform.Client, accountID int64, body map[string]any) bool {
	for k := range body {
		if !grokRedeemBodyKeys[k] {
			delete(body, k)
		}
	}
	tok := str(body["redemption_token"])
	previewCode, err := db.PreviewTokenCode(tok)
	if err != nil {
		if !errors.Is(err, db.ErrCardnetNotFound) {
			log.Printf("[grok-promo] preview token %s: %v", shortTok(tok), err)
		}
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "卡密校验已失效，请重新输入卡密再兑换。卡密没有被消耗。", "error_code": grokTokenErrorCode})
		return false
	}
	plan, promo := grokPromoPlan(str(body["code"]), previewCode)
	if !promo {
		return true
	}
	return grokPromoRecheck(c, cli, accountID, plan, body)
}

// grokPreflightRejected 卡台预检自己没通过（HTTP 非 2xx、业务码非 0、带 blocking_reason）。
// 这种情况卡台的原因更根本，半价校验不再插话。
func grokPreflightRejected(status int, raw json.RawMessage) (reason string, rejected bool) {
	m := jsonMap(raw)
	msg := pickNested(m, "error", "msg", "message")
	if status < 200 || status >= 300 {
		return msg, true
	}
	if c, ok := m["code"].(float64); ok && c != 0 {
		return msg, true
	}
	if r := pickNested(m, "blocking_reason"); r != "" {
		return r, true
	}
	return "", false
}

// grokPromoVerdict 预检通过后，半价码能否兑到这个账号；返回非空即拦截文案。
func grokPromoVerdict(raw json.RawMessage, plan string) string {
	q, err := cardplatform.ParseGrokPromoQuote(raw, plan)
	if err != nil {
		log.Printf("[grok-promo] %s quote: %v", plan, err)
		return grokPromoUnknownMsg
	}
	if q.HalfPrice() {
		return ""
	}
	label := nonEmptyStr(cardplatform.GrokPlanLabel(cardplatform.GrokPromoKey(plan)), "Grok 首月半价")
	return fmt.Sprintf("这是 %s 卡密，只能兑换到有首月半价资格的 Grok 账号。这个账号首月要付 %s（原价 %s），没有半价资格。"+
		"卡密没有被消耗，请换一个有首月半价资格的账号，或改用正价卡密。",
		label, formatMinorAmount(q.AmountMinor, q.Currency), formatMinorAmount(q.ListAmountMinor, q.Currency))
}

func rejectGrokPromo(c *gin.Context, msg string) {
	c.JSON(http.StatusUnprocessableEntity, gin.H{"error": msg, "error_code": grokPromoErrorCode})
}

// grokPromoRecheck 半价码提交兑换前在服务端重新预检。Grok 兑换不带 preflight_token，
// 只靠预检时的拦截可以被直接调 /redeem 绕过。返回 false 时已写响应。
func grokPromoRecheck(c *gin.Context, cli *cardplatform.Client, accountID int64, plan string, body map[string]any) bool {
	pf := map[string]any{"credential": body["credential"]}
	for _, k := range []string{"code", "redemption_token"} {
		if v := str(body[k]); v != "" {
			pf[k] = v
		}
	}
	injectRedeemCardPolicy(pf, accountID)
	st, raw, err := cli.Preflight(c.Request.Context(), pf, deviceFrom(c))
	if err != nil {
		log.Printf("[grok-promo] recheck preflight: %v", err)
		rejectGrokPromo(c, grokPromoUnknownMsg)
		return false
	}
	if reason, rejected := grokPreflightRejected(st, raw); rejected {
		// 卡台的 4xx（如 409 码已被兑换）原样给前端，它据此决定要不要去查单；
		// 5xx 不能原样给：前端会当成「可能已扣款」去轮询，而这次其实没提交。
		if st >= 400 && st < 500 {
			proxyPublicJSON(c, st, raw)
			return false
		}
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": nonEmptyStr(reason, "Grok 账号预检未通过，卡密没有被消耗。"), "error_code": grokPreflightErrorCode,
		})
		return false
	}
	if msg := grokPromoVerdict(raw, plan); msg != "" {
		rejectGrokPromo(c, msg)
		return false
	}
	return true
}

func formatMinorAmount(minor int64, currency string) string {
	if currency == "" || currency == "USD" {
		return fmt.Sprintf("$%d.%02d", minor/100, minor%100)
	}
	return fmt.Sprintf("%d.%02d %s", minor/100, minor%100, currency)
}
