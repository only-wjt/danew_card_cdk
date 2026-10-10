package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/danew/cdk-recharge-system/internal/db"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

func settingOrEnv(key, env string) string {
	if db.DB != nil {
		if v, _ := db.GetSetting(key); strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return strings.TrimSpace(os.Getenv(env))
}

// enabled 后台保存的配置优先，没填才用服务器环境变量。
func enabled() (token, chatID string, ok bool) {
	token = settingOrEnv("telegram_token", "TELEGRAM_BOT_TOKEN")
	chatID = settingOrEnv("telegram_chat_id", "TELEGRAM_CHAT_ID")
	return token, chatID, token != "" && chatID != ""
}

// SendText sends a Telegram message asynchronously. No-op if not configured.
func SendText(text string) {
	go func() {
		if err := SendNow(text); err != nil && err.Error() != "telegram not configured" {
			log.Printf("[telegram] send failed: %v", err)
		}
	}()
}

// SendNow 立刻发送，并把失败原因返回给调用方。未配置时返回错误。
func SendNow(text string) error {
	return sendNow(context.Background(), text)
}

var errorURL = regexp.MustCompile(`(?i)https?://[^\s<>"']+`)

// Sanitize before truncating: neither transport URLs nor echoed credentials may
// reach logs, the test endpoint, or the durable queue's last_error column.
func safeTelegramError(message, token string) string {
	if token != "" {
		for _, secret := range []string{token, url.QueryEscape(token), url.PathEscape(token)} {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	message = errorURL.ReplaceAllString(message, "[url redacted]")
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > 300 {
		message = message[:300]
	}
	return message
}

func sendNow(ctx context.Context, text string) error {
	token, chatID, ok := enabled()
	if !ok {
		return fmt.Errorf("telegram not configured")
	}
	fail := func(message string) error { return fmt.Errorf("%s", safeTelegramError(message, token)) }
	api := "https://api.telegram.org/bot" + token + "/sendMessage"
	form := url.Values{}
	form.Set("chat_id", chatID)
	form.Set("text", text)
	form.Set("parse_mode", "HTML")
	form.Set("disable_web_page_preview", "true")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("telegram request could not be created")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := *httpClient
	// Never forward a credential-bearing request to a redirect destination.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		// Transport errors can embed arbitrary URLs and escaped credentials.
		// Keep a useful category without exposing their untrusted detail.
		if ctx.Err() != nil {
			return fmt.Errorf("telegram request canceled or timed out")
		}
		return fmt.Errorf("telegram network request failed")
	}
	defer resp.Body.Close()
	const maxBody = 64 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return fmt.Errorf("telegram HTTP %d response read failed", resp.StatusCode)
	}
	if len(body) > maxBody {
		return fmt.Errorf("telegram HTTP %d response too large", resp.StatusCode)
	}
	var result struct {
		OK          bool   `json:"ok"`
		Code        int    `json:"error_code"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("telegram HTTP %d invalid JSON response", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !result.OK {
		return fail(fmt.Sprintf("telegram HTTP %d code %d: %s", resp.StatusCode, result.Code, result.Description))
	}
	return nil
}

// NotifyNewOrder formats and sends a "new order" notification.
// An order = a CDK was used and a session was submitted (ConfirmRechargeTask).
func NotifyNewOrder(taskID, cdkCode, sessionJSON string) {
	email := extractAccountEmail(sessionJSON)
	if email == "" {
		email = "（待后台确认）"
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	text := fmt.Sprintf(
		"🔔 <b>新订单</b>\n"+
			"🆔 任务号: <code>%s</code>\n"+
			"🎟 CDK: <code>%s</code>\n"+
			"📧 账号: %s\n"+
			"🕐 %s\n"+
			"——\n"+
			"请登录后台处理: https://gpt.claudec.ai",
		escapeHTML(taskID), escapeHTML(cdkCode), escapeHTML(email), now,
	)
	SendText(text)
}

// Redeemed 通知一笔会员已经开通。不带卡密。调用方只在状态第一次变成已开通时调用。
func Redeemed(product, plan, who, amount string) {
	SendText(FormatRedeemed(product, plan, who, amount) + "\n" + time.Now().Format("2006-01-02 15:04:05"))
}

// EnqueueRedeemed persists a success notification; it never sends HTTP.
func EnqueueRedeemed(key, product, plan, who, amount string) error {
	return db.EnqueueTelegramNotification(key, FormatRedeemed(product, plan, who, amount))
}

// FormatRedeemed is pure, so callers can enqueue its text inside their own transaction.
func FormatRedeemed(product, plan, who, amount string) string {
	who = strings.TrimSpace(who)
	if who == "" {
		who = "—"
	}
	amount = strings.TrimSpace(amount)
	if amount == "" {
		amount = "—"
	}
	return fmt.Sprintf(
		"✅ <b>%s开通成功</b>\n"+
			"套餐: %s\n"+
			"账号: %s\n"+
			"金额: %s",
		escapeHTML(product), escapeHTML(plan), escapeHTML(who), escapeHTML(amount),
	)
}

// ChatGPTRedeemed 通知 ChatGPT 已经开通。地区用「菲区」「美区」这种说法。
func ChatGPTRedeemed(plan, email, code, region string) {
	SendText(formatChatGPTRedeemed(plan, email, code, region))
}

// EnqueueChatGPTRedeemed persists the formatted message without HTTP delivery.
func EnqueueChatGPTRedeemed(key, plan, email, code, region string) error {
	return db.EnqueueTelegramNotification(key, formatChatGPTRedeemed(plan, email, code, region))
}

// EnqueueChatGPTRedeemedAliases persists one notification for related business keys without HTTP delivery.
func EnqueueChatGPTRedeemedAliases(keys []string, plan, email, code, region string) error {
	return db.EnqueueTelegramNotificationAliases(keys, formatChatGPTRedeemed(plan, email, code, region))
}

func formatChatGPTRedeemed(plan, email, code, region string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		email = "—"
	}
	plan = strings.TrimSpace(plan)
	if plan == "" {
		plan = "—"
	}
	code = strings.TrimSpace(code)
	if code == "" {
		code = "—"
	}
	region = strings.TrimSpace(region)
	if region == "" {
		region = "待同步"
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	text := fmt.Sprintf(
		"✅ <b>ChatGPT 开通成功</b>\n"+
			"套餐: %s\n"+
			"账号: %s\n"+
			"卡密: <code>%s</code>\n"+
			"地区: %s\n"+
			"🕐 %s",
		escapeHTML(plan), escapeHTML(email), escapeHTML(code), escapeHTML(region), now,
	)
	return text
}

// RegionLabel 把付款地区码说成运营能看懂的区。空字符串是发码时的默认菲律宾；还没同步到的不要猜。
func RegionLabel(country *string) string {
	if country == nil {
		return "待同步"
	}
	switch strings.ToUpper(strings.TrimSpace(*country)) {
	case "":
		return "菲区"
	case "PH":
		return "菲区"
	case "US":
		return "美区"
	case "JP":
		return "日区"
	case "KR":
		return "韩区"
	case "CL":
		return "智利"
	case "EG":
		return "埃及"
	case "IN":
		return "印度"
	case "NG":
		return "尼日利亚"
	case "TR":
		return "土耳其"
	default:
		return strings.ToUpper(strings.TrimSpace(*country))
	}
}

// extractAccountEmail best-effort parses an account email from a ChatGPT session JSON blob.
func extractAccountEmail(sessionJSON string) string {
	sessionJSON = strings.TrimSpace(sessionJSON)
	if sessionJSON == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(sessionJSON), &m); err != nil {
		return ""
	}
	// common shapes: {"user":{"email":...}} or {"email":...} or {"account":{"email":...}}
	if u, ok := m["user"].(map[string]any); ok {
		if e, ok := u["email"].(string); ok && e != "" {
			return e
		}
	}
	if a, ok := m["account"].(map[string]any); ok {
		if e, ok := a["email"].(string); ok && e != "" {
			return e
		}
	}
	if e, ok := m["email"].(string); ok && e != "" {
		return e
	}
	return ""
}

func escapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
