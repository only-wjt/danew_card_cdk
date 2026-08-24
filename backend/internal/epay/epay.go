package epay

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// Config 标准易支付（彩虹/码支付/七相聚合等）商户配置。
type Config struct {
	APIBase  string // 如 https://api.payqixiang.cn （无尾斜杠）
	PID      string
	Key      string
	SignMode string // append（默认，七相/彩虹）| key_param（少数平台 &key=）
}

func (c Config) Ready() bool {
	return strings.TrimSpace(c.APIBase) != "" &&
		strings.TrimSpace(c.PID) != "" &&
		strings.TrimSpace(c.Key) != ""
}

func (c Config) signMode() string {
	m := strings.ToLower(strings.TrimSpace(c.SignMode))
	if m == "key_param" {
		return "key_param"
	}
	return "append"
}

// BuildPayURL 生成跳转支付的 GET URL（submit.php）。
func (c Config) BuildPayURL(outTradeNo, name, money, notifyURL, returnURL, payType string) string {
	params := map[string]string{
		"pid":          strings.TrimSpace(c.PID),
		"type":         strings.TrimSpace(payType),
		"out_trade_no": strings.TrimSpace(outTradeNo),
		"notify_url":   strings.TrimSpace(notifyURL),
		"return_url":   strings.TrimSpace(returnURL),
		"name":         truncate(name, 127),
		"money":        strings.TrimSpace(money),
	}
	params["sign"] = SignWithMode(params, c.Key, c.signMode())
	params["sign_type"] = "MD5"
	return strings.TrimRight(c.APIBase, "/") + "/submit.php?" + encodeForm(params)
}

// VerifyNotify 校验异步通知签名与支付成功状态。
func (c Config) VerifyNotify(params map[string]string) (ok bool, errMsg string) {
	if params == nil {
		return false, "empty params"
	}
	got := strings.TrimSpace(params["sign"])
	if got == "" {
		return false, "missing sign"
	}
	expect := SignWithMode(params, c.Key, c.signMode())
	if !strings.EqualFold(got, expect) {
		return false, "invalid sign"
	}
	st := strings.TrimSpace(params["trade_status"])
	if st != "" && st != "TRADE_SUCCESS" {
		return false, "trade not success"
	}
	return true, ""
}

// Sign 易支付 MD5（默认 append 模式，兼容 payqixiang/七相/彩虹）。
func Sign(params map[string]string, merchantKey string) string {
	return SignWithMode(params, merchantKey, "append")
}

// SignWithMode 生成签名。append: md5(a=b&c=d + KEY)；key_param: md5(a=b&c=d&key=KEY)。
func SignWithMode(params map[string]string, merchantKey string, mode string) string {
	base := signBaseString(params)
	key := strings.TrimSpace(merchantKey)
	if strings.ToLower(strings.TrimSpace(mode)) == "key_param" {
		return md5Hex(base + "&key=" + key)
	}
	return md5Hex(base + key)
}

func signBaseString(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		k = strings.TrimSpace(k)
		if k == "" || k == "sign" || k == "sign_type" {
			continue
		}
		if strings.TrimSpace(v) == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(strings.TrimSpace(params[k]))
	}
	return b.String()
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func encodeForm(m map[string]string) string {
	vals := url.Values{}
	for k, v := range m {
		vals.Set(k, v)
	}
	return vals.Encode()
}

func truncate(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n])
}

// MoneyYuan 分 → 易支付 money 字符串（两位小数）。
func MoneyYuan(cents int64) string {
	if cents < 0 {
		cents = 0
	}
	yuan := float64(cents) / 100.0
	return fmt.Sprintf("%.2f", yuan)
}

// ParsePayTypes 解析已开通的易支付通道（逗号分隔），默认仅支付宝。
func ParsePayTypes(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{"alipay"}
	}
	seen := map[string]bool{}
	out := make([]string, 0, 2)
	for _, part := range strings.Split(raw, ",") {
		p := strings.ToLower(strings.TrimSpace(part))
		if p != "alipay" && p != "wxpay" {
			continue
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	if len(out) == 0 {
		return []string{"alipay"}
	}
	return out
}

// AllowedPayType 判断支付方式是否在已开通列表中。
func AllowedPayType(payType string, allowed []string) bool {
	payType = strings.ToLower(strings.TrimSpace(payType))
	for _, p := range allowed {
		if p == payType {
			return true
		}
	}
	return false
}

// ParseMoneyYuan 易支付 money → 分。
func ParseMoneyYuan(money string) (int64, error) {
	money = strings.TrimSpace(money)
	if money == "" {
		return 0, fmt.Errorf("empty money")
	}
	var yuan float64
	if _, err := fmt.Sscanf(money, "%f", &yuan); err != nil {
		return 0, err
	}
	return int64(yuan*100 + 0.5), nil
}
