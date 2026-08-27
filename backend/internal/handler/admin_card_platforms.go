package handler

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/danew/cdk-recharge-system/internal/provider"
)

// AdminListCardPlatforms GET /api/v1/admin/card-platforms
func AdminListCardPlatforms(c *gin.Context) {
	accounts, err := db.ListCardPlatformAccounts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 顺手暴露哪些账户构造不出 adapter：协议没接入时开双发只会整单失败。
	_, skipped, _ := provider.LoadRegistry()
	sharedURL := cardPlatformWebhookURL(c)
	origin := strings.TrimSuffix(sharedURL, "/api/v1/webhooks/cardplatform")
	out := make([]gin.H, 0, len(accounts))
	for _, a := range accounts {
		accountURL := db.AccountWebhookPublicURL(origin, a.WebhookPath, a.ID)
		out = append(out, gin.H{
			"id": a.ID, "name": a.Name, "protocol": a.Protocol, "site_base": a.SiteBase,
			"status": a.Status, "priority": a.Priority,
			"is_primary_default": a.IsPrimaryDefault, "force_new_card": a.ForceNewCard,
			"has_credential":     strings.TrimSpace(a.CredSecret) != "",
			"has_webhook_secret": strings.TrimSpace(a.WebhookSecret) != "",
			"webhook_secret_hint": maskSecret(a.WebhookSecret),
			"webhook_path":        a.WebhookPath,
			"webhook_url":         accountURL,
			"circuit_state":       a.CircuitState, "circuit_fail_count": a.CircuitFailCount,
			"last_ok_at": a.LastOKAt, "last_error": a.LastError, "last_error_at": a.LastErrorAt,
		})
	}
	legacy, _ := db.GetSetting("webhook_secret")
	c.JSON(http.StatusOK, gin.H{
		"accounts":           out,
		"unusable":           skipped,
		"dual_bind":          siteDualBindEnabled(),
		"allow_single":       allowDegradedSingleBind(),
		"eligible_issuer":    len(accounts),
		"webhook_url":        sharedURL,
		"legacy_secret_set":  strings.TrimSpace(legacy) != "",
		"legacy_secret_hint": maskSecret(legacy),
	})
}

// AdminUpsertCardPlatform POST /api/v1/admin/card-platforms/upsert
func AdminUpsertCardPlatform(c *gin.Context) {
	var req struct {
		ID               int64  `json:"id"`
		Name             string `json:"name"`
		Protocol         string `json:"protocol"`
		SiteBase         string `json:"site_base"`
		CredPublic       string `json:"cred_public"`
		CredSecret       string `json:"cred_secret"`
		WebhookSecret    string `json:"webhook_secret"`
		Status           string `json:"status"`
		Priority         int    `json:"priority"`
		IsPrimaryDefault bool   `json:"is_primary_default"`
		ForceNewCard     bool   `json:"force_new_card"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	id, err := db.UpsertCardPlatformAccount(db.CardPlatformAccount{
		ID: req.ID, Name: strings.TrimSpace(req.Name), Protocol: strings.TrimSpace(req.Protocol),
		SiteBase: strings.TrimSpace(req.SiteBase), CredPublic: strings.TrimSpace(req.CredPublic),
		CredSecret: strings.TrimSpace(req.CredSecret), WebhookSecret: strings.TrimSpace(req.WebhookSecret),
		Status: strings.TrimSpace(req.Status), Priority: req.Priority,
		IsPrimaryDefault: req.IsPrimaryDefault, ForceNewCard: req.ForceNewCard,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "upsert_card_platform", fmt.Sprintf("id=%d name=%s priority=%d", id, req.Name, req.Priority))
	AdminListCardPlatforms(c)
}

// AdminSetCardPlatformWebhookSecret POST /api/v1/admin/card-platforms/webhook-secret
func AdminSetCardPlatformWebhookSecret(c *gin.Context) {
	var req struct {
		ID            int64  `json:"id"`
		WebhookSecret string `json:"webhook_secret"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
		return
	}
	if err := db.SetCardPlatformWebhookSecret(req.ID, req.WebhookSecret); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "set_card_platform_webhook_secret", fmt.Sprintf("id=%d", req.ID))
	AdminListCardPlatforms(c)
}

// AdminSetCardPlatformWebhookURL POST /api/v1/admin/card-platforms/webhook-url
func AdminSetCardPlatformWebhookURL(c *gin.Context) {
	var req struct {
		ID         int64  `json:"id"`
		WebhookURL string `json:"webhook_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
		return
	}
	if err := db.SetCardPlatformWebhookPath(req.ID, req.WebhookURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "set_card_platform_webhook_url", fmt.Sprintf("id=%d", req.ID))
	AdminListCardPlatforms(c)
}

// AdminSetCardPlatformStatus POST /api/v1/admin/card-platforms/status
func AdminSetCardPlatformStatus(c *gin.Context) {
	var req struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	id := req.ID
	if strings.EqualFold(strings.TrimSpace(req.Status), "disabled") {
		// 停用的台上如果还挂着未兑换的绑定，那些本站码就只剩单台可兑，得先让站长知道。
		if n, err := db.CountBindingsByAccount(id); err == nil && n > 0 {
			auditAdmin(c, "disable_card_platform_with_pending",
				fmt.Sprintf("id=%d pending_bindings=%d", id, n))
		}
	}
	if err := db.SetCardPlatformAccountStatus(id, req.Status); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "set_card_platform_status", fmt.Sprintf("id=%d status=%s", id, req.Status))
	AdminListCardPlatforms(c)
}

// AdminResetCardPlatformCircuit POST /api/v1/admin/card-platforms/reset-circuit
func AdminResetCardPlatformCircuit(c *gin.Context) {
	var req struct {
		ID int64 `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if err := db.ResetCardPlatformCircuit(req.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "reset_card_platform_circuit", fmt.Sprintf("id=%d", req.ID))
	AdminListCardPlatforms(c)
}

// AdminPingCardPlatform POST /api/v1/admin/card-platforms/ping
// 按账户凭证探测连通（双卡台各自白名单 / Key 可能不同）。
func AdminPingCardPlatform(c *gin.Context) {
	var req struct {
		ID int64 `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
		return
	}
	_, acc, err := provider.ForAccount(req.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	base := strings.TrimRight(strings.TrimSpace(acc.SiteBase), "/")
	base = strings.TrimSuffix(base, "/openapi/v1")
	base = strings.TrimSuffix(base, "/openapi")
	if base == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "site_base 未配置"})
		return
	}
	key := strings.TrimSpace(acc.CredSecret)
	cfg := cardplatform.Config{SiteBase: base, APIKey: key}
	client := &http.Client{Timeout: 8 * time.Second}
	candidates := []string{
		cfg.OpenAPIBase() + "/balance",
		base + "/health",
		cfg.PublicCDKBase(),
		base + "/",
	}
	var probed string
	var status int
	var lastErr string
	for _, u := range candidates {
		httpReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, u, nil)
		if err != nil {
			lastErr = err.Error()
			continue
		}
		httpReq.Header.Set("User-Agent", "cdk-recharge-system/cardplatform-ping")
		if key != "" && strings.Contains(u, "/openapi/") {
			httpReq.Header.Set("X-API-Key", key)
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			lastErr = err.Error()
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if resp.StatusCode > 0 && resp.StatusCode < 500 {
			probed = u
			status = resp.StatusCode
			break
		}
		lastErr = "status " + resp.Status
	}
	egressIP, _, _ := detectEgressIP(c.Request.Context())
	if probed == "" {
		c.JSON(http.StatusBadGateway, gin.H{
			"ok": false, "error": "unreachable", "detail": lastErr,
			"account_id": req.ID, "name": acc.Name, "site_base": base,
			"egress_ip": egressIP,
		})
		return
	}
	msg := "reachable"
	if status == 401 {
		msg = "主机可达；API Key 无效（401）"
	} else if status == 403 {
		msg = "主机可达；可能 IP 不在白名单（403）"
	}
	var spendable string
	if key != "" {
		if bal, berr := cardplatform.New(cfg).GetBalance(c.Request.Context()); berr == nil && bal != nil {
			spendable = string(bal.SpendableBalance)
		}
	}
	if status >= 200 && status < 500 && status != 401 && status != 403 {
		_ = db.MarkCardPlatformAccountOK(acc.ID)
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": true, "message": msg, "probed": probed, "status": status,
		"account_id": req.ID, "name": acc.Name, "site_base": base,
		"spendable_usd": spendable, "egress_ip": egressIP,
	})
}

// AdminPutDualBindConfig PUT /api/v1/admin/card-platforms/dual-bind
func AdminPutDualBindConfig(c *gin.Context) {
	var req struct {
		Enabled     bool `json:"enabled"`
		AllowSingle bool `json:"allow_single"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.Enabled {
		// 只有一个可用台时双发退化成单绑，等于没有容灾还多花一倍钱，直接挡住。
		reg, skipped, err := provider.LoadRegistry()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if len(reg.Providers) < 2 && !req.AllowSingle {
			msg := "至少需要 2 个可用卡台账户才能开启双绑发码"
			if len(skipped) > 0 {
				msg += "（不可用：" + strings.Join(skipped, "; ") + "）"
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}
	}
	if err := db.SetSetting(settingDualBindEnabled, boolSetting(req.Enabled)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := db.SetSetting(settingDualBindDegraded, boolSetting(req.AllowSingle)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "set_site_dual_bind", fmt.Sprintf("enabled=%v allow_single=%v", req.Enabled, req.AllowSingle))
	AdminListCardPlatforms(c)
}

func boolSetting(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// AdminGetSiteCDKBindings GET /api/v1/admin/card-platforms/bindings?code=DN-xxx
// 排障用：这张本站码在各台的绑定与状态（不返回上游完整码）。
func AdminGetSiteCDKBindings(c *gin.Context) {
	code := strings.TrimSpace(c.Query("code"))
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code required"})
		return
	}
	row, ok := db.GetSiteCDKByCode(code)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "卡密不存在"})
		return
	}
	bindings, err := db.ListBindingsForSiteCodeID(row.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	activeID, _ := db.GetActiveBindingID(row.ID)
	c.JSON(http.StatusOK, gin.H{
		"code": row.Code, "plan": row.Plan, "code_kind": row.CodeKind,
		"issue_status": row.IssueStatus, "dual_eligible": row.DualEligible,
		"status": row.Status, "active_binding_id": activeID,
		"bindings": bindings,
	})
}
