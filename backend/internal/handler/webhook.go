package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/gin-gonic/gin"
)

// CardPlatformWebhook POST /api/v1/webhooks/cardplatform
// 以及 POST /api/v1/webhooks/cardplatform/:accountId、/api/v1/webhooks/avanfinity
// 验签兼容旧台 HMAC(secret, body) 与 Avanfinity HMAC(secret, ts + '.' + body)。
func CardPlatformWebhook(c *gin.Context) {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	wantAccountID := resolveWebhookAccountID(c)
	accounts, _ := db.ListCardPlatformAccounts()
	type webhookCredential struct {
		accountID int64
		secret    string
	}
	credentials := make([]webhookCredential, 0, len(accounts)+1)
	for _, acc := range accounts {
		if wantAccountID > 0 && acc.ID != wantAccountID {
			continue
		}
		if secret := strings.TrimSpace(acc.WebhookSecret); secret != "" {
			credentials = append(credentials, webhookCredential{accountID: acc.ID, secret: secret})
		}
	}
	if wantAccountID <= 0 {
		if legacySecret, _ := db.GetSetting("webhook_secret"); strings.TrimSpace(legacySecret) != "" {
			credentials = append(credentials, webhookCredential{secret: strings.TrimSpace(legacySecret)})
		}
	}
	if len(credentials) == 0 {
		log.Printf("webhook: webhook_secret not configured account=%d", wantAccountID)
		c.Status(http.StatusServiceUnavailable)
		return
	}
	gots := webhookSignatureCandidates(c)
	if len(gots) == 0 {
		log.Printf("webhook: missing signature header account=%d", wantAccountID)
		c.Status(http.StatusUnauthorized)
		return
	}
	timestamps := webhookTimestamps(c)
	var matchedAccountID int64
	matched := false
	for _, credential := range credentials {
		if !webhookSignatureMatches(credential.secret, raw, timestamps, gots) {
			continue
		}
		matched = true
		if credential.accountID > 0 {
			matchedAccountID = credential.accountID
		}
		break
	}
	if !matched {
		log.Printf("webhook: signature mismatch account=%d headers=%d", wantAccountID, len(gots))
		c.Status(http.StatusUnauthorized)
		return
	}
	if matchedAccountID == 0 {
		if primary, err := db.LegacyCardPlatformAccount(); err == nil {
			matchedAccountID = primary.ID
		}
	}

	// 尽快 200；入库 best-effort
	var payload map[string]interface{}
	_ = json.Unmarshal(raw, &payload)

	eventType := ""
	if v, ok := payload["event"].(string); ok {
		eventType = v
	}
	if v, ok := payload["type"].(string); ok && eventType == "" {
		eventType = v
	}
	// 幂等键
	idem := webhookIdemKey(payload, eventType)
	if err := db.InsertWebhookEvent(matchedAccountID, eventType, idem, string(raw)); err != nil {
		// 唯一冲突视为已处理（幂等）
		if !strings.Contains(err.Error(), "UNIQUE") && !strings.Contains(err.Error(), "unique") {
			log.Printf("webhook store: %v", err)
		}
	} else {
		// 若是 GPT 直充完成，可写审计
		if eventType == "gpt_direct.completed" {
			db.WriteAudit("webhook", "gpt_direct.completed", idem, c.ClientIP())
		}
	}
	// 本站 CDK 状态：兑换完成 → consumed（避免列表仍显示「未使用」）
	if strings.HasPrefix(strings.ToLower(eventType), "gpt_direct.") {
		applyCDKStatusFromWebhook(payload, eventType, matchedAccountID)
	}
	// 卡健康：失败/成功终态观察（同卡多邮箱失败 → 拉黑）
	if strings.HasPrefix(strings.ToLower(eventType), "gpt_direct.") {
		observeFromWebhookPayload(payload, matchedAccountID)
	}
	c.Status(http.StatusOK)
}

// applyCDKStatusFromWebhook 根据卡台终态回写本站 SQLite 中的 CDK status。
func applyCDKStatusFromWebhook(payload map[string]interface{}, eventType string, accountID int64) {
	if payload == nil {
		return
	}
	cdkID := anyToInt64(payload["cdk_id"])
	if cdkID <= 0 {
		return
	}
	et := strings.ToLower(strings.TrimSpace(eventType))
	st := strings.ToLower(strings.TrimSpace(strAny(payload["cdk_status"])))
	if st == "" {
		if strings.Contains(et, "completed") {
			st = "consumed"
		} else {
			return
		}
	}
	// 失败回传 unused 时，勿把已 consumed/disabled 降级回去
	if st == "unused" || st == "reserved" {
		if cur := db.GetCardplatformCDKStatus(cdkID); cur == "consumed" || cur == "disabled" {
			return
		}
	}
	if binding, ok := db.FindSiteBindingByRemote(accountID, strconv.FormatInt(cdkID, 10)); ok {
		if st == "consumed" {
			_ = db.UpdateBindingStatus(binding.ID, db.BindingStatusConsumed, "")
			_ = db.UpdateCardplatformCDKStatusByRowID(binding.SiteCodeID, st)
			_ = db.MarkSiteCDKFulfilled(binding.SiteCodeID, accountID, binding.Provider, !binding.IsPrimary, "webhook")
		}
		return
	}
	if code, ok := db.LookupCardplatformCDKCode(cdkID, strAny(payload["code_prefix"])); ok {
		if owner, err := db.CardPlatformAccountForLegacyCode(code); err == nil &&
			accountID > 0 && owner.ID != accountID {
			return
		}
	} else {
		legacy, _ := db.LegacyCardPlatformAccount()
		if accountID > 0 && legacy.ID != accountID {
			return
		}
	}
	_ = db.UpdateCardplatformCDKStatus(cdkID, st)
}

func webhookIdemKey(p map[string]interface{}, eventType string) string {
	str := func(k string) string {
		if v, ok := p[k]; ok {
			switch t := v.(type) {
			case string:
				return t
			case float64:
				return strings.TrimSuffix(strings.TrimSuffix(jsonNumber(t), ".0"), ".")
			}
		}
		return ""
	}
	switch eventType {
	case "card_transaction":
		return strings.Join([]string{eventType, str("auth_id"), str("type"), str("status")}, "|")
	case "card_operation":
		return strings.Join([]string{eventType, str("operation"), str("operation_id"), str("status")}, "|")
	case "gpt_direct.completed":
		if id := str("order_id"); id != "" {
			return "gpt_direct.completed|order|" + id
		}
		if id := str("client_request_id"); id != "" {
			return "gpt_direct.completed|client|" + id
		}
	}
	// fallback
	h := sha256.Sum256([]byte(str("auth_id") + str("order_id") + str("operation_id") + eventType + time.Now().UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(h[:16])
}

func jsonNumber(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

// AdminListWebhooks GET /api/v1/admin/webhooks/events
func AdminListWebhooks(c *gin.Context) {
	limit := 100
	accountID, _ := strconv.ParseInt(strings.TrimSpace(c.Query("account_id")), 10, 64)
	rows, err := db.ListWebhookEvents(accountID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list failed"})
		return
	}
	accounts, _ := db.ListCardPlatformAccounts()
	names := map[int64]string{}
	for _, a := range accounts {
		names[a.ID] = a.Name
	}
	// 脱敏：不返回完整 card_number
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		name := names[r.AccountID]
		if name == "" && r.AccountID == 0 {
			name = "未归属"
		} else if name == "" {
			name = "卡台 #" + strconv.FormatInt(r.AccountID, 10)
		}
		out = append(out, gin.H{
			"id":           r.ID,
			"account_id":   r.AccountID,
			"account_name": name,
			"event_type":   r.EventType,
			"idem_key":     r.IdemKey,
			"created_at":   r.CreatedAt,
			"payload":      sanitizeWebhookPayload(r.Payload),
		})
	}
	urlHint := cardPlatformWebhookURL(c)
	origin := strings.TrimSuffix(urlHint, "/api/v1/webhooks/cardplatform")
	accOut := make([]gin.H, 0, len(accounts))
	anySet := false
	for _, a := range accounts {
		set := strings.TrimSpace(a.WebhookSecret) != ""
		if set {
			anySet = true
		}
		accountURL := db.AccountWebhookPublicURL(origin, a.WebhookPath, a.ID)
		accOut = append(accOut, gin.H{
			"id":                  a.ID,
			"name":                a.Name,
			"site_base":           a.SiteBase,
			"status":              a.Status,
			"is_primary_default":  a.IsPrimaryDefault,
			"has_webhook_secret":  set,
			"webhook_secret_hint": maskSecret(a.WebhookSecret),
			"webhook_url":         accountURL,
		})
	}
	legacy, _ := db.GetSetting("webhook_secret")
	legacySet := strings.TrimSpace(legacy) != ""
	if legacySet {
		anySet = true
	}
	c.JSON(http.StatusOK, gin.H{
		"events":              out,
		"webhook_url":         urlHint,
		"accounts":            accOut,
		"any_secret_set":      anySet,
		"legacy_secret_set":   legacySet,
		"legacy_secret_hint":  maskSecret(legacy),
		"webhook_secret_set":  anySet,
		"webhook_secret_hint": maskSecret(legacy),
	})
}

func resolveWebhookAccountID(c *gin.Context) int64 {
	if id, err := strconv.ParseInt(strings.TrimSpace(c.Param("accountId")), 10, 64); err == nil && id > 0 {
		return id
	}
	hook := strings.Trim(strings.TrimSpace(c.Param("hookPath")), "/")
	if hook == "" || hook == "cardplatform" {
		return 0
	}
	full := "/api/v1/webhooks/" + hook
	if acc, err := db.FindCardPlatformAccountByWebhookPath(full); err == nil {
		return acc.ID
	}
	return 0
}

func cardPlatformWebhookURL(c *gin.Context) string {
	host := strings.TrimSpace(c.Request.Host)
	if host == "" {
		return ""
	}
	scheme := "https"
	if proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); proto != "" {
		scheme = proto
	} else if c.Request.TLS == nil {
		scheme = "http"
	}
	return scheme + "://" + host + "/api/v1/webhooks/cardplatform"
}

func sanitizeWebhookPayload(raw string) interface{} {
	var m map[string]interface{}
	if json.Unmarshal([]byte(raw), &m) != nil {
		return raw
	}
	if cn, ok := m["card_number"].(string); ok && len(cn) > 4 {
		m["card_number"] = "****" + cn[len(cn)-4:]
	}
	return m
}
