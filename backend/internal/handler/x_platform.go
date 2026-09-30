package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/danew/cdk-recharge-system/internal/avanfinity"
	"github.com/danew/cdk-recharge-system/internal/db"
)

func xClient(acc db.CardPlatformAccount) *avanfinity.Client {
	return &avanfinity.Client{
		Base:   acc.SiteBase,
		AppID:  acc.CredPublic,
		Secret: acc.CredSecret,
	}
}

func loadXAccount(c *gin.Context, id int64) (db.CardPlatformAccount, bool) {
	if id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
		return db.CardPlatformAccount{}, false
	}
	acc, err := db.GetCardPlatformAccount(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return db.CardPlatformAccount{}, false
	}
	if acc.Protocol != db.AccountProtocolAvanfinityAPIv1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "这不是 X 会员卡台"})
		return db.CardPlatformAccount{}, false
	}
	return acc, true
}

// AdminProbeXAccount POST /api/v1/admin/card-platforms/probe-x
// 按顺序测凭证、X 权限、卡片、写接口白名单。失败停在那一步。
func AdminProbeXAccount(c *gin.Context) {
	var req struct {
		ID int64 `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	acc, ok := loadXAccount(c, req.ID)
	if !ok {
		return
	}
	result := avanfinity.Probe(c.Request.Context(), xClient(acc))
	reason := ""
	if !result.OK && len(result.Steps) > 0 {
		last := result.Steps[len(result.Steps)-1]
		reason = last.Title + "：" + last.Detail
	}
	_ = db.NoteCardPlatformResult(acc.ID, result.OK, reason)
	egress, _, _ := detectEgressIP(c.Request.Context())
	// 卡片明细给付款卡页用；测试码内容不在响应里。
	c.JSON(http.StatusOK, gin.H{
		"ok":               result.OK,
		"steps":            result.Steps,
		"balance":          result.Balance,
		"payments_enabled": result.PaymentsEnabled,
		"plan_count":       result.PlanCount,
		"cards":            result.Cards,
		"egress_ip":        egress,
		"account_id":       acc.ID,
	})
}

// AdminXAccountCards GET /api/v1/admin/card-platforms/x-cards?id=1
// 不用路径参数：card-platforms 下面已经有 bindings 等静态段，再挂 :id 会和它们冲突。
func AdminXAccountCards(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Query("id"), 10, 64)
	acc, ok := loadXAccount(c, id)
	if !ok {
		return
	}
	client := xClient(acc)
	cards, err := client.ListCards(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	bal, _ := client.GetBalance(c.Request.Context())
	spendable := ""
	if bal != nil {
		spendable = bal.Balance
	}
	c.JSON(http.StatusOK, gin.H{"cards": cards, "balance": spendable})
}

// AdminXCalls GET /api/v1/admin/card-platforms/x-calls?id=
func AdminXCalls(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Query("id"), 10, 64)
	if _, ok := loadXAccount(c, id); !ok {
		return
	}
	rows, err := db.ListUpstreamCalls(id, 40)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"calls": rows})
}

// AdminReorderCardPlatforms POST /api/v1/admin/card-platforms/reorder
func AdminReorderCardPlatforms(c *gin.Context) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if err := db.ReorderOpenAIAccounts(req.IDs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "reorder_card_platforms", strings.Trim(strings.ReplaceAll(strconvInt64s(req.IDs), " ", ","), "[]"))
	AdminListCardPlatforms(c)
}

func strconvInt64s(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

// AdminGetXConfig GET /api/v1/admin/x/config
func AdminGetXConfig(c *gin.Context) {
	channels, err := db.ListXChannels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	limits, err := db.ListXPlanLimits("")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	wallet, _ := db.GetSetting("x_alert_wallet_usd")
	card, _ := db.GetSetting("x_alert_card_usd")
	stuck, _ := db.GetSetting("x_alert_stuck_minutes")
	samples, _ := db.ListLatestQuoteSamples()
	c.JSON(http.StatusOK, gin.H{
		"channels": channels,
		"limits":   limits,
		"samples":  samples,
		"alerts": gin.H{
			"wallet_usd":    wallet,
			"card_usd":      card,
			"stuck_minutes": stuck,
		},
	})
}

// AdminSaveXChannel PUT /api/v1/admin/x/channels
func AdminSaveXChannel(c *gin.Context) {
	var ch db.XChannel
	if err := c.ShouldBindJSON(&ch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if err := db.SaveXChannel(ch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "save_x_channel", ch.Channel)
	AdminGetXConfig(c)
}

// AdminSaveXLimits PUT /api/v1/admin/x/plan-limits
func AdminSaveXLimits(c *gin.Context) {
	var req struct {
		Limits []db.XPlanLimit `json:"limits"`
		Alerts struct {
			WalletUSD    string `json:"wallet_usd"`
			CardUSD      string `json:"card_usd"`
			StuckMinutes string `json:"stuck_minutes"`
		} `json:"alerts"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if err := db.SaveXPlanLimits(req.Limits); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	_ = db.SetSetting("x_alert_wallet_usd", strings.TrimSpace(req.Alerts.WalletUSD))
	_ = db.SetSetting("x_alert_card_usd", strings.TrimSpace(req.Alerts.CardUSD))
	_ = db.SetSetting("x_alert_stuck_minutes", strings.TrimSpace(req.Alerts.StuckMinutes))
	auditAdmin(c, "save_x_plan_limits", "")
	AdminGetXConfig(c)
}
