package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
			log.Printf("[cdk-preflight] bind session failed: %v", berr)
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
	clientIDs := []string{firstNonEmpty(strAny(body["clientRequestId"]), strAny(body["client_request_id"]))}
	if route.Provider.Protocol() == provider.ProtocolAvanfinity202608 {
		effectiveID := provider.AvanfinityRedeemClientRequestID(clientIDs[0])
		body["clientRequestId"] = effectiveID
		clientIDs = append(clientIDs, effectiveID)
	}
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
	// Notification errors are local, not an upstream rejection: never release
	// the binding or invite another recharge submission after acceptance.
	if enqueueImmediateGPTResult(c, route.Account.ID, siteCode, body, st, raw, clientIDs...) {
		return
	}
	proxyPublicJSON(c, st, raw)
}

// A redeem HTTP success/envelope success only acknowledges acceptance. Notify
// here only for an explicit terminal order state; otherwise polling owns it.
func enqueueImmediateGPTResult(c *gin.Context, accountID int64, code string, body map[string]any, st int, raw []byte, extraClientIDs ...string) bool {
	if st < 200 || st >= 300 {
		return false
	}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil || payload == nil {
		writeGPTAcceptedRetry(c)
		return true
	}
	status, explicit := gptOrderStatus(payload)
	if !explicit || !gptSuccessStatus(status) {
		return false
	}
	// A flat envelope "success" is only acceptance; aliases such as success,
	// succeeded and done are terminal only on an explicit order/object state.
	if status != "completed" && !gptExplicitOrderSuccess(payload) {
		return false
	}
	clientIDs := append([]string{strAny(body["client_request_id"]), strAny(body["clientRequestId"])}, extraClientIDs...)
	if err := notifyGPTSuccess(accountID, code, gptResultEmail(payload), payload, clientIDs...); err != nil {
		writeGPTAcceptedRetry(c)
		return true
	}
	return false
}

func gptExplicitOrderSuccess(payload map[string]any) bool {
	data, _ := payload["data"].(map[string]any)
	obj, _ := data["object"].(map[string]any)
	rootObj, _ := payload["object"].(map[string]any)
	for _, container := range []map[string]any{obj, data, rootObj, payload} {
		if order, ok := container["order"].(map[string]any); ok {
			if status, explicit := gptOrderStatus(order); explicit {
				return gptSuccessStatus(status)
			}
		}
	}
	for _, object := range []map[string]any{obj, rootObj} {
		if status, explicit := gptOrderStatus(object); explicit {
			return gptSuccessStatus(status)
		}
	}
	return false
}

func writeGPTAcceptedRetry(c *gin.Context) {
	// Do not echo the completed order: existing clients apply response state
	// before checking HTTP status and would stop polling on that terminal state.
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"error":      "兑换已受理，通知保存暂时失败；请查询结果，勿重新提交兑换",
		"error_code": "GPT_NOTIFICATION_RETRY_RESULT",
		"status":     "queued", "redeem_accepted": true, "retry_action": "result",
	})
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
// 兑换页读的是 data.order.status，这里必须看同一层，否则页面已成功也不会发通知。
func isTerminalRedeemSuccess(payload map[string]any) bool {
	status, explicit := gptOrderStatus(payload)
	if explicit {
		return gptSuccessStatus(status)
	}
	return isGPTDirectCompleted(webhookEventType(payload))
}

// Ordered from the actual order out to its response/event envelope. An explicit
// inner state always wins, including pending/failed under an envelope success.
func gptPayloadScopes(payload map[string]any) []map[string]any {
	data, _ := payload["data"].(map[string]any)
	obj, _ := data["object"].(map[string]any)
	rootObj, _ := payload["object"].(map[string]any)
	scopes := []map[string]any{}
	for _, container := range []map[string]any{obj, data, rootObj, payload} {
		if order, ok := container["order"].(map[string]any); ok {
			scopes = append(scopes, order)
		}
	}
	return append(scopes, obj, rootObj, data, payload)
}

func gptOrderStatus(payload map[string]any) (string, bool) {
	for _, scope := range gptPayloadScopes(payload) {
		status := ""
		for _, key := range []string{"status", "order_status", "state"} {
			if v := strings.ToLower(strings.TrimSpace(strAny(scope[key]))); v != "" {
				// Contradictory aliases in one order are not proof of completion.
				if !gptSuccessStatus(v) {
					return v, true
				}
				status = v
			}
		}
		if status != "" {
			return status, true
		}
	}
	return "", false
}

func gptSuccessStatus(status string) bool {
	switch status {
	case "completed", "success", "succeeded", "done":
		return true
	}
	return false
}

func gptOrderFromPayload(payload map[string]any) map[string]any {
	for _, scope := range gptPayloadScopes(payload) {
		if scope != nil {
			return scope
		}
	}
	return nil
}

func gptPayloadField(payload map[string]any, keys ...string) string {
	for _, scope := range gptPayloadScopes(payload) {
		for _, key := range keys {
			if v := strings.TrimSpace(strAny(scope[key])); v != "" {
				return v
			}
		}
	}
	return ""
}

func gptOrderID(payload map[string]any) string {
	scopes := gptPayloadScopes(payload)
	for i, scope := range scopes {
		if id := firstNonEmpty(strAny(scope["orderId"]), strAny(scope["order_id"])); id != "" {
			return id
		}
		// The root event id is not an order id. Nested order/object ids and
		// legacy polling root ids are safe; client_request_id is not canonical.
		if id := strings.TrimSpace(strAny(scope["id"])); id != "" && !strings.HasPrefix(id, "evt_") &&
			(i != len(scopes)-1 || webhookEventType(payload) == "") {
			return id
		}
	}
	return ""
}

func gptResultEmail(payload map[string]any) string {
	return gptPayloadField(payload, "account_email", "email")
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

// notificationAccountID only resolves when the caller has no explicit route.
// A known site code must never silently acquire the legacy/default namespace.
func notificationAccountID(accountID int64, code string) (int64, error) {
	if accountID > 0 {
		return accountID, nil
	}
	if db.DB == nil {
		return 0, errors.New("notification database unavailable")
	}
	if provider.IsSiteCode(code) {
		route, err := provider.ResolveSticky(code)
		if err != nil {
			return 0, err
		}
		return route.Account.ID, nil
	}
	if strings.TrimSpace(code) != "" {
		var owner int64
		err := db.DB.QueryRow(`SELECT COALESCE(fulfilled_account_id,0) FROM cardplatform_cdk_codes
			WHERE code = ? COLLATE NOCASE AND code_kind = 'legacy' LIMIT 1`, strings.TrimSpace(code)).Scan(&owner)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		if owner > 0 {
			return owner, nil
		}
	}
	// Distinguish missing legacy configuration from a failed account read.
	if _, err := db.ListCardPlatformAccounts(); err != nil {
		return 0, err
	}
	if acc, err := db.LegacyCardPlatformAccount(); err == nil {
		return acc.ID, nil
	}
	return 0, nil // genuinely unknown legacy owner
}

func gptNotificationKey(accountID int64, orderID string) string {
	return fmt.Sprintf("gpt:account:%d:order:%s", accountID, strings.TrimSpace(orderID))
}

// All observed identities travel together so a richer observation learns the
// canonical order alias without queuing another delivery for an earlier fallback.
func gptNotificationKeys(accountID int64, code string, payload map[string]any, extraClientIDs ...string) []string {
	keys := []string{}
	if orderID := gptOrderID(payload); orderID != "" {
		keys = append(keys, gptNotificationKey(accountID, orderID))
	}
	clientIDs := append([]string{gptPayloadField(payload, "client_request_id", "clientRequestId")}, extraClientIDs...)
	seenClients := map[string]bool{}
	for _, clientID := range clientIDs {
		clientID = strings.TrimSpace(clientID)
		if clientID == "" || seenClients[clientID] {
			continue
		}
		seenClients[clientID] = true
		keys = append(keys, fmt.Sprintf("gpt:account:%d:client:%s", accountID, clientID))
	}
	if normalized := strings.ToUpper(strings.TrimSpace(code)); normalized != "" {
		// Keep full CDKs out of persisted queue identity strings as well as logs.
		hash := sha256.Sum256([]byte(normalized))
		keys = append(keys, fmt.Sprintf("gpt:account:%d:code:%x", accountID, hash))
	}
	return keys
}

// A webhook may echo a site's remote code; polling uses the public site code.
// Resolve that exact account-qualified binding before deriving a code alias.
func gptNotificationCode(accountID int64, payload map[string]any) (string, error) {
	code := gptPayloadField(payload, "cdk_code", "cdkCode", "code")
	if code == "" || db.DB == nil {
		return code, nil
	}
	var siteCode string
	err := db.DB.QueryRow(`SELECT site_code FROM site_cdk_bindings
		WHERE account_id = ? AND remote_code = ? COLLATE NOCASE LIMIT 1`, accountID, strings.TrimSpace(code)).Scan(&siteCode)
	if err == nil {
		return siteCode, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return code, nil
}

func notifyGPTSuccess(accountID int64, code, email string, payload map[string]any, extraClientIDs ...string) error {
	if !isTerminalRedeemSuccess(payload) {
		return nil
	}
	accountID, err := notificationAccountID(accountID, code)
	if err != nil {
		return err
	}
	aliasCode := code
	if aliasCode == "" {
		aliasCode, err = gptNotificationCode(accountID, payload)
		if err != nil {
			return err
		}
	}
	// Only route-bound caller codes may mutate local CDK state; a webhook's
	// echoed code is an identity alias, not authority for a metadata claim.
	keys := gptNotificationKeys(accountID, aliasCode, payload, extraClientIDs...)
	if len(keys) == 0 {
		return errors.New("completed GPT order missing order, client or code identity")
	}
	plan := gptPayloadField(payload, "plan")
	country := countryFromCurrency(gptPayloadField(payload, "currency"))
	// Read metadata independently of the old consumed/notice claim flag. Never
	// mutate that flag before the durable queue insert has succeeded.
	if code != "" {
		if db.DB == nil {
			return errors.New("notification database unavailable")
		}
		var storedPlan string
		var storedCountry sql.NullString
		err := db.DB.QueryRow("SELECT COALESCE(plan,''), payment_country FROM cardplatform_cdk_codes WHERE code = ? COLLATE NOCASE LIMIT 1", strings.TrimSpace(code)).Scan(&storedPlan, &storedCountry)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if storedPlan != "" {
			plan = storedPlan
		}
		if storedCountry.Valid {
			v := storedCountry.String
			country = &v
		}
	}
	displayCode := strings.TrimSpace(code)
	if displayCode == "" {
		displayCode = strings.TrimSpace(aliasCode)
	}
	if email == "" {
		email = gptResultEmail(payload)
	}
	if email == "" && displayCode != "" {
		sess, sessErr := db.GetSessionByCDK(displayCode)
		if sessErr != nil {
			return sessErr
		}
		email = extractEmailFromSession(sess)
	}
	if err := notify.EnqueueChatGPTRedeemedAliases(keys, gptPlanLabel(plan), email, displayCode, notify.RegionLabel(country)); err != nil {
		return err
	}
	if code != "" {
		db.ClaimCDKSuccessNotice(code)
	}
	return nil
}

func notifyChatGPTCompleted(accountID int64, payload map[string]any) error {
	return notifyGPTSuccess(accountID, "", gptResultEmail(payload), payload)
}

func webhookDataObject(payload map[string]any) map[string]any {
	data, _ := payload["data"].(map[string]any)
	if obj, ok := data["object"].(map[string]any); ok {
		return obj
	}
	if obj, ok := payload["object"].(map[string]any); ok {
		return obj
	}
	if data != nil {
		return data
	}
	return payload
}

func nestedStr(obj map[string]any, key, child string) string {
	inner, _ := obj[key].(map[string]any)
	if inner == nil {
		return ""
	}
	return strAny(inner[child])
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
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
