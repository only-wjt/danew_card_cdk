package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/danew/cdk-recharge-system/internal/db"
)

const agentCDKPolicyKey = "agent_cdk_policy"

// AgentCDKPolicy 代理卡密渠道策略。
type AgentCDKPolicy struct {
	// BlockPublicRedeem=true → 已分配给代理的卡密不允许在公开兑换页自助兑换，
	// 避免同一张码被客户和代理同时用掉。默认开。
	BlockPublicRedeem bool `json:"block_public_redeem"`
}

func defaultAgentCDKPolicy() AgentCDKPolicy {
	return AgentCDKPolicy{BlockPublicRedeem: true}
}

func loadAgentCDKPolicy() AgentCDKPolicy {
	p := defaultAgentCDKPolicy()
	raw, err := db.GetSetting(agentCDKPolicyKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return p
	}
	_ = json.Unmarshal([]byte(raw), &p)
	return p
}

func saveAgentCDKPolicy(p AgentCDKPolicy) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return db.SetSetting(agentCDKPolicyKey, string(b))
}

// guardLocalStockCDK 公开兑换拦截本站库存账号（GPT白号），无论是否已分配。
func guardLocalStockCDK(c *gin.Context, code string) bool {
	if strings.TrimSpace(code) == "" {
		return false
	}
	row, ok := db.LookupStoredCDKDetail(code)
	if !ok || !db.IsLocalStockPlan(row.Plan) {
		return false
	}
	c.JSON(http.StatusForbidden, gin.H{
		"error":      "该账号为 GPT白号，不能在公开页兑换，请联系代理发放。",
		"error_code": "CDK_LOCAL_STOCK",
	})
	return true
}

// guardAgentAssignedCDK 公开兑换入口拦截：已分配代理的卡密不给客户自助兑换。
// 返回 true 表示已写响应，调用方应直接 return。
func guardAgentAssignedCDK(c *gin.Context, code string) bool {
	if guardLocalStockCDK(c, code) {
		return true
	}
	if strings.TrimSpace(code) == "" {
		return false
	}
	if !loadAgentCDKPolicy().BlockPublicRedeem {
		return false
	}
	row, ok := db.LookupStoredCDKDetail(code)
	if !ok || row.AssignedAgentUserID <= 0 {
		return false
	}
	// 代理已经提交过这张码：客户多半是来看进度的，文案要能让兑换页走「恢复进度」分支
	switch strings.ToLower(strings.TrimSpace(row.Status)) {
	case "reserved", "consumed":
		c.JSON(http.StatusForbidden, gin.H{
			"error":      "该卡密已由代理渠道提交充值（已使用），可直接查询充值进度。",
			"error_code": "CDK_AGENT_CHANNEL_USED",
		})
	default:
		c.JSON(http.StatusForbidden, gin.H{
			"error":      "该卡密由代理渠道发出，请联系你的购买方代为充值。",
			"error_code": "CDK_AGENT_CHANNEL",
		})
	}
	return true
}

// AdminGetAgentCDKPolicy GET /api/v1/admin/agent-policy
func AdminGetAgentCDKPolicy(c *gin.Context) {
	c.JSON(http.StatusOK, loadAgentCDKPolicy())
}

// AdminPutAgentCDKPolicy PUT /api/v1/admin/agent-policy
func AdminPutAgentCDKPolicy(c *gin.Context) {
	var body struct {
		BlockPublicRedeem *bool `json:"block_public_redeem"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	p := loadAgentCDKPolicy()
	if body.BlockPublicRedeem != nil {
		p.BlockPublicRedeem = *body.BlockPublicRedeem
	}
	if err := saveAgentCDKPolicy(p); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	auditAdmin(c, "agent_cdk_policy_update", "block_public_redeem="+boolText(p.BlockPublicRedeem))
	c.JSON(http.StatusOK, p)
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
