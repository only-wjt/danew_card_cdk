package handler

import (
	"net/http"
	"strings"

	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/danew/cdk-recharge-system/internal/provider"
	"github.com/gin-gonic/gin"
)

// PublicCDKGraceRecovery POST /api/v1/public/cdk/recover-subscription
// 只用持卡人的 redemption/preflight token，不带本站特权 API Key。
// 本站码回 preview 时选定的那台；旧码回发码账户。
func PublicCDKGraceRecovery(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 80<<10)
	var body struct {
		RedemptionToken string `json:"redemption_token"`
		PreflightToken  string `json:"preflight_token"`
		Confirmed       bool   `json:"confirmed"`
	}
	if c.ShouldBindJSON(&body) != nil || !body.Confirmed || strings.TrimSpace(body.RedemptionToken) == "" ||
		len(body.RedemptionToken) > 512 || strings.TrimSpace(body.PreflightToken) == "" || len(body.PreflightToken) > 65536 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先检测账号并确认取消宽限期原订阅"})
		return
	}
	payload := map[string]any{
		"redemption_token": strings.TrimSpace(body.RedemptionToken),
		"preflight_token":  strings.TrimSpace(body.PreflightToken),
		"confirmed":        true,
	}
	boundCode, _ := db.FindCodeByRedemptionToken(body.RedemptionToken)
	if provider.IsSiteCode(boundCode) {
		route, rerr := provider.ResolveSticky(boundCode)
		if rerr != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": rerr.Error()})
			return
		}
		status, raw, err := route.Provider.RecoverSubscription(c.Request.Context(), payload, deviceFrom(c))
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "暂时无法确认处理结果，请重新检测账号，不要重复取消"})
			return
		}
		proxyPublicJSON(c, status, maskUpstreamCode(raw, route.RemoteCode, boundCode))
		return
	}
	cli := legacyCardClientForCode(boundCode)
	status, raw, err := cli.RecoverSubscription(c.Request.Context(), payload, deviceFrom(c))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "暂时无法确认处理结果，请重新检测账号，不要重复取消"})
		return
	}
	proxyPublicJSON(c, status, raw)
}
