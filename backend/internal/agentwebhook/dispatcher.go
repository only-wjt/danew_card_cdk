// Package agentwebhook 负责把代理事件投递到代理自己配置的回调地址。
//
// 事件由业务侧写进 agent_webhook_deliveries（outbox），本包的 worker 单协程消费。
// 业务路径不做同步 HTTP，代理端再慢也拖不垮充值流程。
package agentwebhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/danew/cdk-recharge-system/internal/db"
)

const (
	pollInterval   = 5 * time.Second
	batchPerTick   = 20
	requestTimeout = 10 * time.Second
	maxAttempts    = 6
	// 响应体只读这么多，代理返回一大坨 HTML 时不至于把日志撑爆。
	maxRespPeek = 512
)

// backoffSec 第 n 次失败后等待多久重试（n 从 1 开始）。
var backoffSec = []int{30, 120, 600, 1800, 7200}

// ---- SSRF 防护 ----
//
// webhook_url 是代理自己填的，等于让外部输入决定服务端往哪发请求。
// 不加限制的话，代理可以拿它探内网、打云厂商元数据接口（169.254.169.254）。
// 这里在「保存配置」和「真正拨号」两处都拦一遍：只查一次 DNS 挡不住
// DNS rebinding，所以拨号时按实际连接地址再判一次才算数。

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return true
	}
	// IPv4 保留段：0.0.0.0/8、100.64.0.0/10（CGNAT）、192.0.0.0/24、198.18.0.0/15
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 0,
			v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127,
			v4[0] == 192 && v4[1] == 0 && v4[2] == 0,
			v4[0] == 198 && (v4[1] == 18 || v4[1] == 19):
			return true
		}
	}
	return false
}

// ValidateURL 校验代理填写的回调地址，供保存设置时做前置拦截。
func ValidateURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil // 留空表示不启用回调
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("回调地址格式不正确")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("回调地址必须是 http 或 https")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("回调地址缺少域名")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("回调地址不能指向内网或保留地址")
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("回调地址域名无法解析")
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return fmt.Errorf("回调地址不能指向内网或保留地址")
		}
	}
	return nil
}

// guardedClient 拨号前按真实连接地址再判一次，堵住 DNS rebinding。
var guardedClient = &http.Client{
	Timeout: requestTimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		// 跟随跳转会绕过上面的地址校验，直接不跟。
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 5 * time.Second,
			Control: func(_, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return err
				}
				if isBlockedIP(net.ParseIP(host)) {
					return fmt.Errorf("blocked address %s", host)
				}
				return nil
			},
		}).DialContext,
		TLSHandshakeTimeout: 5 * time.Second,
		MaxIdleConns:        16,
	},
}

// Sign 生成回调签名：hex(HMAC-SHA256(secret, timestamp + "." + body))。
// 把时间戳纳入签名是为了让代理侧能拒绝重放的旧请求。
func Sign(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// Start 启动后台投递 worker，随 ctx 结束退出。
func Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				drain(ctx)
			}
		}
	}()
}

func drain(ctx context.Context) {
	due, err := db.ListDueAgentWebhooks(batchPerTick)
	if err != nil {
		log.Printf("[agent-webhook] query due failed: %v", err)
		return
	}
	for _, d := range due {
		select {
		case <-ctx.Done():
			return
		default:
		}
		deliver(ctx, d)
	}
}

func deliver(ctx context.Context, d db.AgentWebhookDelivery) {
	target, secret, err := db.GetAgentWebhookTarget(d.AgentUserID)
	if err != nil {
		fail(d, 0, "读取代理回调配置失败："+err.Error())
		return
	}
	if target == "" {
		fail(d, 0, "代理未配置回调地址")
		return
	}
	if secret == "" {
		fail(d, 0, "代理未生成回调签名密钥")
		return
	}

	body := []byte(d.Payload)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		fail(d, 0, "构造请求失败："+err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "danew-agent-webhook/1")
	req.Header.Set("X-Webhook-Id", d.EventID)
	req.Header.Set("X-Webhook-Event", d.EventType)
	req.Header.Set("X-Webhook-Timestamp", ts)
	req.Header.Set("X-Signature", Sign(secret, ts, body))

	resp, err := guardedClient.Do(req)
	if err != nil {
		retryOrFail(d, 0, err.Error())
		return
	}
	defer resp.Body.Close()
	peek := make([]byte, maxRespPeek)
	n, _ := resp.Body.Read(peek)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := db.MarkAgentWebhookDelivered(d.ID, resp.StatusCode); err != nil {
			log.Printf("[agent-webhook] mark delivered %s failed: %v", d.EventID, err)
		}
		return
	}
	retryOrFail(d, resp.StatusCode, fmt.Sprintf("HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(peek[:n]))))
}

func retryOrFail(d db.AgentWebhookDelivery, code int, msg string) {
	// d.Attempts 是本次投递之前的次数，落库时会 +1
	if d.Attempts+1 >= maxAttempts {
		fail(d, code, msg+"（已达最大重试次数）")
		return
	}
	wait := backoffSec[len(backoffSec)-1]
	if d.Attempts < len(backoffSec) {
		wait = backoffSec[d.Attempts]
	}
	if err := db.MarkAgentWebhookRetry(d.ID, code, msg, wait); err != nil {
		log.Printf("[agent-webhook] mark retry %s failed: %v", d.EventID, err)
	}
}

func fail(d db.AgentWebhookDelivery, code int, msg string) {
	if err := db.MarkAgentWebhookFailed(d.ID, code, msg); err != nil {
		log.Printf("[agent-webhook] mark failed %s failed: %v", d.EventID, err)
	}
}
