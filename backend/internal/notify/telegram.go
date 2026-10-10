package notify

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
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// enabled reports whether Telegram notification is configured.
func enabled() (token, chatID string, ok bool) {
	token = strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	chatID = strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID"))
	return token, chatID, token != "" && chatID != ""
}

// SendText sends a Telegram message asynchronously. No-op if not configured.
func SendText(text string) {
	token, chatID, ok := enabled()
	if !ok {
		return
	}
	go func() {
		api := "https://api.telegram.org/bot" + token + "/sendMessage"
		form := url.Values{}
		form.Set("chat_id", chatID)
		form.Set("text", text)
		form.Set("parse_mode", "HTML")
		form.Set("disable_web_page_preview", "true")
		resp, err := httpClient.PostForm(api, form)
		if err != nil {
			log.Printf("[telegram] send failed: %v", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			log.Printf("[telegram] non-200: %d %s", resp.StatusCode, string(body))
		}
	}()
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

// Redeemed 通知一笔会员已经开通。调用方只在状态第一次变成已开通时调用。
func Redeemed(product, plan, who, code, amount string) {
	who = strings.TrimSpace(who)
	if who == "" {
		who = "—"
	}
	amount = strings.TrimSpace(amount)
	if amount == "" {
		amount = "—"
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	text := fmt.Sprintf(
		"✅ <b>%s开通成功</b>\n"+
			"套餐: %s\n"+
			"账号: %s\n"+
			"卡密: <code>%s</code>\n"+
			"金额: %s\n"+
			"🕐 %s",
		escapeHTML(product), escapeHTML(plan), escapeHTML(who), escapeHTML(code), escapeHTML(amount), now,
	)
	SendText(text)
}

// ChatGPTRedeemed 通知一张 ChatGPT 卡密已经开通。地区用「菲区」「美区」这种说法。
func ChatGPTRedeemed(plan, email, code, region string) {
	email = strings.TrimSpace(email)
	if email == "" {
		email = "—"
	}
	plan = strings.TrimSpace(plan)
	if plan == "" {
		plan = "—"
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
	SendText(text)
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
