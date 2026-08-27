package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/danew/cdk-recharge-system/internal/plansync"
	"github.com/gin-gonic/gin"
)

// AdminGetCardSelectionRules GET /api/v1/admin/card-selection/rules
// 返回选卡优先级规则列表（含实时产品在线状态）
func AdminGetCardSelectionRules(c *gin.Context) {
	accountID, _ := strconv.ParseInt(c.Query("account_id"), 10, 64)
	var rules []db.CardSelectionRule
	var err error
	if accountID > 0 {
		rules, err = db.GetCardSelectionRulesForAccount(accountID)
	} else {
		rules, err = db.GetCardSelectionRules()
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	statusMap := map[string]db.PlanStatusCache{}
	if accountID > 0 {
		statusMap, _ = db.GetPlanStatusCacheMapForAccount(accountID)
	} else {
		statusMap, _ = db.GetPlanStatusCacheMap()
	}

	type ruleView struct {
		db.CardSelectionRule
		Online        bool    `json:"online"`
		SyncedAt      string  `json:"synced_at"`
		ServiceFeeUSD float64 `json:"service_fee_usd"`
	}

	out := make([]ruleView, 0, len(rules))
	for _, r := range rules {
		rv := ruleView{CardSelectionRule: r, Online: true}
		if ps, ok := statusMap[r.PlanKey]; ok {
			rv.Online = ps.Online
			rv.SyncedAt = ps.SyncedAt
			rv.ServiceFeeUSD = ps.ServiceFeeUSD
		}
		out = append(out, rv)
	}

	var statuses []db.PlanStatusCache
	if accountID > 0 {
		statuses, _ = db.GetPlanStatusCacheForAccount(accountID)
	} else {
		statuses, _ = db.GetPlanStatusCache()
	}
	lastSync := latestSyncTime(statuses)

	c.JSON(http.StatusOK, gin.H{
		"rules":      out,
		"last_sync":  lastSync,
		"next_sync":  nextSyncIn(lastSync),
		"account_id": accountID,
	})
}

// AdminPutCardSelectionRules PUT /api/v1/admin/card-selection/rules
// 整体替换选卡规则配置（顺序 = 优先级）
func AdminPutCardSelectionRules(c *gin.Context) {
	var body struct {
		AccountID int64                  `json:"account_id"`
		Rules     []db.CardSelectionRule `json:"rules"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if _, err := db.GetCardPlatformAccount(body.AccountID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择有效的卡台账户"})
		return
	}
	for i := range body.Rules {
		body.Rules[i].PlanKey = strings.TrimSpace(body.Rules[i].PlanKey)
		body.Rules[i].DisplayName = strings.TrimSpace(body.Rules[i].DisplayName)
		if body.Rules[i].PlanKey == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "plan_key required for each rule"})
			return
		}
		if body.Rules[i].DisplayName == "" {
			body.Rules[i].DisplayName = body.Rules[i].PlanKey
		}
		body.Rules[i].SortOrder = i + 1
	}
	saveErr := db.SetCardSelectionRulesForAccount(body.AccountID, body.Rules)
	if saveErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": saveErr.Error()})
		return
	}
	auditAdmin(c, "update_card_selection_rules", fmt.Sprintf("account=%d count=%d", body.AccountID, len(body.Rules)))
	if body.AccountID > 0 {
		q := c.Request.URL.Query()
		q.Set("account_id", strconv.FormatInt(body.AccountID, 10))
		c.Request.URL.RawQuery = q.Encode()
	}
	AdminGetCardSelectionRules(c)
}

// AdminGetPlanStatus GET /api/v1/admin/card-selection/plan-status
// 返回产品状态缓存（含最后同步时间 + 预计下次同步时间）
// AdminGetPlanStatus GET /api/v1/admin/card-selection/plan-status
// 返回逻辑套餐状态缓存 + 实体产品缓存（含最后同步时间）
func AdminGetPlanStatus(c *gin.Context) {
	accountID, _ := strconv.ParseInt(c.Query("account_id"), 10, 64)
	var statuses []db.PlanStatusCache
	var products []db.CardProductCache
	var err error
	if accountID > 0 {
		statuses, err = db.GetPlanStatusCacheForAccount(accountID)
		products, _ = db.GetCardProductsForAccount(accountID)
	} else {
		statuses, err = db.GetPlanStatusCache()
		products, _ = db.GetCardProducts()
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	lastSync := latestSyncTime(statuses)
	if lastSync == "" {
		lastSync = latestProductSyncTime(products)
	}
	c.JSON(http.StatusOK, gin.H{
		"statuses":   statuses,
		"products":   products,
		"last_sync":  lastSync,
		"next_sync":  nextSyncIn(lastSync),
		"account_id": accountID,
	})
}

// AdminSyncPlanStatus POST /api/v1/admin/card-selection/sync
// 立即触发一次产品状态同步（主动同步）
func AdminSyncPlanStatus(c *gin.Context) {
	accountID, _ := strconv.ParseInt(c.Query("account_id"), 10, 64)
	if accountID > 0 {
		r, err := plansync.SyncNowForAccount(c.Request.Context(), accountID)
		if err != nil {
			writeCardErr(c, err)
			return
		}
		auditAdmin(c, "sync_plan_status", fmt.Sprintf("account=%d plans=%d products=%d", accountID, r.Plans, r.Products))
		q := c.Request.URL.Query()
		q.Set("account_id", strconv.FormatInt(accountID, 10))
		c.Request.URL.RawQuery = q.Encode()
		AdminGetPlanStatus(c)
		return
	}
	cfg := cardplatform.LoadConfig()
	if cfg.APIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "card_api_key not configured"})
		return
	}
	r, err := plansync.SyncNow(c.Request.Context())
	if err != nil {
		writeCardErr(c, err)
		return
	}
	auditAdmin(c, "sync_plan_status", fmt.Sprintf("plans=%d products=%d", r.Plans, r.Products))
	AdminGetPlanStatus(c)
}

// latestSyncTime 从套餐状态列表中取最新的 synced_at。
func latestSyncTime(statuses []db.PlanStatusCache) string {
	var latest string
	for _, s := range statuses {
		if latest == "" || s.SyncedAt > latest {
			latest = s.SyncedAt
		}
	}
	return latest
}

// latestProductSyncTime 从产品列表中取最新的 synced_at。
func latestProductSyncTime(products []db.CardProductCache) string {
	var latest string
	for _, p := range products {
		if latest == "" || p.SyncedAt > latest {
			latest = p.SyncedAt
		}
	}
	return latest
}

// nextSyncIn 计算距离下次自动同步的剩余时间描述。
func nextSyncIn(lastSync string) string {
	if lastSync == "" {
		return "—"
	}
	t, err := time.Parse("2006-01-02 15:04:05", lastSync)
	if err != nil {
		return "—"
	}
	next := t.Add(3 * time.Minute)
	rem := time.Until(next)
	if rem <= 0 {
		return "即将同步"
	}
	if rem < time.Minute {
		return fmt.Sprintf("%ds 后", int(rem.Seconds()))
	}
	return fmt.Sprintf("%dm%ds 后", int(rem.Minutes()), int(rem.Seconds())%60)
}

// AdminGetSiteRedeemPolicy GET /api/v1/admin/card-selection/site-policy
func AdminGetSiteRedeemPolicy(c *gin.Context) {
	accountID, _ := strconv.ParseInt(c.Query("account_id"), 10, 64)
	p := loadSiteRedeemPolicyForAccount(accountID)
	issuer, segType, segKey := resolveIssueCardPrefForAccount(accountID, p)
	c.JSON(http.StatusOK, gin.H{
		"policy": p,
		"resolved_pref": gin.H{
			"issuer": issuer, "segment_type": segType, "segment_key": segKey,
		},
		"account_id": accountID,
		"note":       "启用后：发码写入 preferred 产品；兑换请求注入 no_auto_card_switch。一卡几付硬限由卡台账户容量策略执行。",
	})
}

// AdminPutSiteRedeemPolicy PUT /api/v1/admin/card-selection/site-policy
func AdminPutSiteRedeemPolicy(c *gin.Context) {
	accountID, _ := strconv.ParseInt(c.Query("account_id"), 10, 64)
	if _, err := db.GetCardPlatformAccount(accountID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择有效的卡台账户"})
		return
	}
	var p SiteRedeemPolicy
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	// 产品映射唯一来源是当前账户的选卡规则，禁止 API 再写一份覆盖值。
	p.ProductCode = ""
	p.Issuer = ""
	if err := saveSiteRedeemPolicyForAccount(accountID, p); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "update_site_redeem_policy", fmt.Sprintf("account=%d enabled=%v no_switch=%v product=%s", accountID, p.Enabled, p.NoAutoCardSwitch, p.ProductCode))
	AdminGetSiteRedeemPolicy(c)
}
