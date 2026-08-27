package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/danew/cdk-recharge-system/internal/gptcheck"
	"github.com/gin-gonic/gin"
)

func accounthubBaseURL() string {
	if u := strings.TrimSpace(os.Getenv("ACCOUNTHUB_BASE_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://localhost:8788"
}

// extractEmailFromSession 从 session JSON 提取 user.email。
func extractEmailFromSession(raw string) string {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "{") {
		return ""
	}
	var data map[string]interface{}
	if json.Unmarshal([]byte(s), &data) != nil {
		return ""
	}
	if user, ok := data["user"].(map[string]interface{}); ok {
		if email, ok := user["email"].(string); ok {
			return strings.TrimSpace(email)
		}
	}
	if email, ok := data["email"].(string); ok {
		return strings.TrimSpace(email)
	}
	return ""
}

type acchubInvoiceResp struct {
	InvoiceURL string                   `json:"invoice_url"`
	Invoices   []map[string]interface{} `json:"invoices"`
	Source     string                   `json:"source"`
	Error      string                   `json:"error"`
}

// queryAccounthubInvoices 通过邮箱调 accounthub 获取账单。
func queryAccounthubInvoices(email string) (*acchubInvoiceResp, error) {
	base := accounthubBaseURL()
	u := fmt.Sprintf("%s/gpt/invoices-by-email?email=%s", base, url.QueryEscape(email))
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("accounthub 请求失败: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.Unmarshal(body, &errResp)
		return nil, fmt.Errorf("accounthub %d: %s", resp.StatusCode, errResp.Error)
	}
	var out acchubInvoiceResp
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("accounthub 响应解析失败")
	}
	return &out, nil
}

// SessionBillingCheck POST /api/v1/public/billing/check
// 支持：
//  1. cdk_code — 用兑换时绑定的 session 查账单
//  2. token_input / session — 直接贴 session / accessToken
func SessionBillingCheck(c *gin.Context) {
	var req struct {
		TokenInput string `json:"token_input"`
		Session    string `json:"session"`
		CDKCode    string `json:"cdk_code"`
		Code       string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	raw := strings.TrimSpace(req.TokenInput)
	if raw == "" {
		raw = strings.TrimSpace(req.Session)
	}
	cdk := strings.TrimSpace(req.CDKCode)
	if cdk == "" {
		cdk = strings.TrimSpace(req.Code)
	}

	source := "session"
	if raw == "" && cdk != "" {
		sess, err := db.GetSessionByCDK(cdk)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询绑定失败"})
			return
		}
		if strings.TrimSpace(sess) == "" {
			// 代理/管理端代充不落 session，只能靠订单里的账号邮箱走 accounthub
			if email := db.AccountEmailByCDK(cdk); email != "" {
				if respondAccounthubInvoices(c, email) {
					return
				}
				c.JSON(http.StatusBadGateway, gin.H{
					"error": "账号 " + email + " 的账单暂时查不到，请稍后重试或联系客服。",
				})
				return
			}
			c.JSON(http.StatusNotFound, gin.H{
				"error": "未找到该卡密的充值记录。请确认卡密正确；若已成功充值，可改用直接粘贴 session 查询。",
			})
			return
		}
		raw = sess
		source = "cdk"
	}

	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入卡密，或粘贴 session JSON / accessToken"})
		return
	}

	// CDK 模式优先走 accounthub：用内部刷新过的 session 查账单
	if source == "cdk" {
		if email := extractEmailFromSession(raw); email != "" {
			if respondAccounthubInvoices(c, email) {
				return
			}
		}
	}

	// 回退：直接用 session/accessToken 查
	res, err := gptcheck.Check(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"summary":     res.Summary,
		"invoices":    res.Invoices,
		"auth_source": source,
	})
}

// respondAccounthubInvoices 用邮箱走 accounthub 查账单并直接写响应；查不到返回 false 由调用方兜底。
func respondAccounthubInvoices(c *gin.Context, email string) bool {
	inv, err := queryAccounthubInvoices(email)
	if err != nil {
		log.Printf("[billing] accounthub fallback for %s: %v", email, err)
		return false
	}
	invoices := inv.Invoices
	if invoices == nil && inv.InvoiceURL != "" {
		invoices = []map[string]interface{}{{"hosted_invoice_url": inv.InvoiceURL}}
	}
	if invoices == nil {
		invoices = []map[string]interface{}{}
	}
	c.JSON(http.StatusOK, gin.H{
		"summary":     map[string]interface{}{"email": email},
		"invoices":    invoices,
		"invoice_url": inv.InvoiceURL,
		"auth_source": "accounthub",
	})
	return true
}

const sessionRefreshMax = 20

// PublicSessionRefresh POST /api/v1/public/session/refresh
// 用 sessionToken 向 ChatGPT 换新 session JSON。不落库。
func PublicSessionRefresh(c *gin.Context) {
	var req struct {
		SessionJSON string   `json:"session_json"`
		TokenInput  string   `json:"token_input"`
		Session     string   `json:"session"`
		Sessions    []string `json:"sessions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	items := make([]string, 0, len(req.Sessions)+1)
	for _, s := range []string{req.SessionJSON, req.TokenInput, req.Session} {
		if t := strings.TrimSpace(s); t != "" {
			items = append(items, t)
			break
		}
	}
	for _, s := range req.Sessions {
		if t := strings.TrimSpace(s); t != "" {
			items = append(items, t)
		}
	}
	if len(items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请粘贴 session JSON 或 sessionToken"})
		return
	}
	if len(items) > sessionRefreshMax {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("一次最多刷新 %d 条", sessionRefreshMax)})
		return
	}

	type row struct {
		OK      bool                   `json:"ok"`
		Email   string                 `json:"email,omitempty"`
		Error   string                 `json:"error,omitempty"`
		Session map[string]interface{} `json:"session,omitempty"`
	}
	out := make([]row, 0, len(items))
	for _, raw := range items {
		data, err := gptcheck.RefreshSession(raw)
		if err != nil {
			out = append(out, row{OK: false, Error: err.Error()})
			continue
		}
		email := firstStringMap(data, "email")
		if email == "" {
			if user, ok := data["user"].(map[string]interface{}); ok {
				email = firstStringMap(user, "email")
			}
		}
		out = append(out, row{OK: true, Email: email, Session: data})
	}
	c.JSON(http.StatusOK, gin.H{"results": out})
}

func firstStringMap(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}
