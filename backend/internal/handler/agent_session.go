package handler

import (
	"net/http"
	"strings"

	"github.com/danew/cdk-recharge-system/internal/gptcheck"
	"github.com/gin-gonic/gin"
)

// AgentCheckSession POST /api/v1/agent/session/check
// 校验 ChatGPT session / accessToken 是否可用，返回邮箱与订阅摘要（不落库、不回显明文 session）。
func AgentCheckSession(c *gin.Context) {
	var req struct {
		Session    string `json:"session"`
		TokenInput string `json:"token_input"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid request", "error_code": "INVALID_REQUEST"})
		return
	}
	raw := strings.TrimSpace(req.Session)
	if raw == "" {
		raw = strings.TrimSpace(req.TokenInput)
	}
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"ok":         false,
			"error":      "请粘贴 session JSON 或 accessToken",
			"error_code": "SESSION_REQUIRED",
		})
		return
	}

	email := extractEmailFromSession(raw)
	res, err := gptcheck.Check(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"ok":         false,
			"error":      err.Error(),
			"error_code": "SESSION_INVALID",
			"email":      email,
		})
		return
	}

	summary := res.Summary
	if summary == nil {
		summary = map[string]interface{}{}
	}
	if email != "" {
		summary["email"] = email
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"email":   email,
		"summary": summary,
	})
}
