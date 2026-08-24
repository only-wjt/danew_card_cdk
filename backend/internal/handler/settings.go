package handler

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/danew/cdk-recharge-system/internal/db"
)

// defaultSkin 未配置站点时的默认皮肤，需与前端 theme.ts 的 DEFAULT_SKIN 保持一致
const defaultSkin = "danew"

// 允许写入 site_settings 的公开安全键（无密钥）
var publicSettingKeys = map[string]bool{
	"brand_name":  true,
	"brand_sub":   true,
	"skin":        true,
	"theme_mode":  true,
}

// 密钥类键：只接受写入，读出脱敏
var secretSettingKeys = map[string]bool{
	"card_api_base":    false,
	"card_api_key":     true,
	"webhook_secret":   true, // 卡台开发者页 whsec_…
	"telegram_token":   true,
	"telegram_chat_id": false,
	"epay_api_base":    false,
	"epay_pid":         false,
	"epay_key":         true,
	"epay_pay_types":   false,
	"public_base_url":  false, // 对外站点根 URL（易支付回调/跳转）
	// agent_swap handled specially (hash)
}

// PublicSiteConfig GET /api/v1/public/site — 用户端拉品牌/皮肤（无鉴权）
func PublicSiteConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"installed":  db.IsInstalled(),
		"brand_name": settingOr("brand_name", "Recharge Portal"),
		"brand_sub":  settingOr("brand_sub", "Account Upgrade Service"),
		"skin":       settingOr("skin", defaultSkin),
		"theme_mode": settingOr("theme_mode", "light"),
	})
}

// AdminGetSettings GET /api/v1/admin/settings
func AdminGetSettings(c *gin.Context) {
	out := gin.H{
		"brand_name": settingOr("brand_name", "Recharge Portal"),
		"brand_sub":  settingOr("brand_sub", "Account Upgrade Service"),
		"skin":       settingOr("skin", defaultSkin),
		"theme_mode": settingOr("theme_mode", "light"),
	}
	// 非密钥可读
	for k, isSecret := range secretSettingKeys {
		v, _ := db.GetSetting(k)
		if isSecret {
			out[k+"_configured"] = strings.TrimSpace(v) != ""
			out[k+"_hint"] = maskSecret(v)
		} else {
			out[k] = v
		}
	}
	out["agent_swap_password_configured"] = agentSwapPasswordConfigured()
	out["public_base_url_effective"] = ResolvePublicBase(c)
	c.JSON(http.StatusOK, out)
}

type adminSettingsBody struct {
	BrandName      *string `json:"brand_name"`
	BrandSub       *string `json:"brand_sub"`
	Skin           *string `json:"skin"`
	ThemeMode      *string `json:"theme_mode"`
	CardAPIBase    *string `json:"card_api_base"`
	CardAPIKey     *string `json:"card_api_key"`
	TelegramToken  *string `json:"telegram_token"`
	TelegramChatID *string `json:"telegram_chat_id"`
	WebhookSecret  *string `json:"webhook_secret"`
	EpayAPIBase    *string `json:"epay_api_base"`
	EpayPID        *string `json:"epay_pid"`
	EpayKey        *string `json:"epay_key"`
	EpayPayTypes   *string `json:"epay_pay_types"` // alipay,wxpay
	PublicBaseURL  *string `json:"public_base_url"` // 易支付 notify/return 根地址，留空=当前访问地址
	// 代理失败换码密码（明文一次写入，存 bcrypt；空=不改）
	AgentSwapPassword *string `json:"agent_swap_password"`
}

// AdminPutSettings PUT /api/v1/admin/settings
func AdminPutSettings(c *gin.Context) {
	var body adminSettingsBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	setIf := func(key string, p *string, max int) error {
		if p == nil {
			return nil
		}
		v := strings.TrimSpace(*p)
		if max > 0 && len(v) > max {
			v = v[:max]
		}
		// 空字符串对密钥类表示「不修改」；若要清空用 " " 不支持清空密钥防误操作
		if v == "" && secretSettingKeys[key] {
			return nil
		}
		return db.SetSetting(key, v)
	}

	allowedSkins := map[string]bool{
		"danew": true,
		"terracotta": true, "ocean": true, "cyber": true, "forest": true, "violet": true,
		"slate": true, "rose": true, "ember": true, "noir": true, "paper": true,
	}
	if body.Skin != nil {
		s := strings.TrimSpace(*body.Skin)
		if !allowedSkins[s] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid skin"})
			return
		}
		_ = db.SetSetting("skin", s)
	}
	if body.ThemeMode != nil {
		m := strings.TrimSpace(*body.ThemeMode)
		if m != "light" && m != "dark" && m != "auto" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid theme_mode"})
			return
		}
		_ = db.SetSetting("theme_mode", m)
	}
	_ = setIf("brand_name", body.BrandName, 40)
	_ = setIf("brand_sub", body.BrandSub, 80)
	_ = setIf("card_api_base", body.CardAPIBase, 200)
	_ = setIf("card_api_key", body.CardAPIKey, 200)
	_ = setIf("telegram_token", body.TelegramToken, 200)
	_ = setIf("telegram_chat_id", body.TelegramChatID, 64)
	_ = setIf("webhook_secret", body.WebhookSecret, 200)
	_ = setIf("epay_api_base", body.EpayAPIBase, 200)
	_ = setIf("epay_pid", body.EpayPID, 64)
	_ = setIf("epay_key", body.EpayKey, 200)
	if body.EpayPayTypes != nil {
		allowed := map[string]bool{}
		for _, p := range strings.Split(strings.ToLower(strings.TrimSpace(*body.EpayPayTypes)), ",") {
			p = strings.TrimSpace(p)
			if p == "alipay" || p == "wxpay" {
				allowed[p] = true
			}
		}
		if len(allowed) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请至少开通一种支付方式（alipay 或 wxpay）"})
			return
		}
		types := make([]string, 0, len(allowed))
		if allowed["alipay"] {
			types = append(types, "alipay")
		}
		if allowed["wxpay"] {
			types = append(types, "wxpay")
		}
		_ = db.SetSetting("epay_pay_types", strings.Join(types, ","))
	}
	if body.PublicBaseURL != nil {
		v := strings.TrimRight(strings.TrimSpace(*body.PublicBaseURL), "/")
		if v == "" {
			_ = db.DeleteSetting("public_base_url")
		} else if !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "站点对外地址须以 http:// 或 https:// 开头"})
			return
		} else {
			_ = db.SetSetting("public_base_url", v)
		}
	}
	if body.AgentSwapPassword != nil {
		pw := strings.TrimSpace(*body.AgentSwapPassword)
		if pw != "" {
			if err := setAgentSwapPassword(pw); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
		}
	}

	auditAdmin(c, "update_settings", "site settings")
	AdminGetSettings(c)
}

func settingOr(key, def string) string {
	v, _ := db.GetSetting(key)
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func maskSecret(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if len(v) <= 4 {
		return "****"
	}
	return "****" + v[len(v)-4:]
}

// ResolvePublicBase 生成对外站点根 URL（无尾斜杠）。
// 优先级：管理端 public_base_url → 环境变量 PUBLIC_BASE_URL → 当前请求 Host（IP/域名+端口）。
func ResolvePublicBase(c *gin.Context) string {
	if v, _ := db.GetSetting("public_base_url"); strings.TrimSpace(v) != "" {
		return strings.TrimRight(strings.TrimSpace(v), "/")
	}
	if v := strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	if c == nil || c.Request == nil {
		return ""
	}
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if xf := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); xf != "" {
		scheme = strings.Split(xf, ",")[0]
	}
	host := c.Request.Host
	if xh := strings.TrimSpace(c.GetHeader("X-Forwarded-Host")); xh != "" {
		host = strings.Split(xh, ",")[0]
	}
	return scheme + "://" + strings.TrimSpace(host)
}
