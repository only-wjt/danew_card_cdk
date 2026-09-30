package xmember

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/danew/cdk-recharge-system/internal/xmember/xlogic"
)

var handleRE = regexp.MustCompile(`^[A-Za-z0-9_]{1,15}$`)

// NormalizeHandle 去掉 @、主页链接和参数，只留 X 用户名。
func NormalizeHandle(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	s = strings.TrimPrefix(s, "www.")
	lower := strings.ToLower(s)
	for _, host := range []string{"x.com/", "twitter.com/"} {
		if strings.HasPrefix(lower, host) {
			s = s[len(host):]
			break
		}
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimPrefix(strings.TrimSpace(s), "@")
}

func ValidHandle(raw string) bool {
	return handleRE.MatchString(NormalizeHandle(raw))
}

// USDToE4 把最多 4 位小数的美元字符串转成整数。失败返回 false。
func USDToE4(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	neg := strings.HasPrefix(raw, "-")
	raw = strings.TrimPrefix(raw, "-")
	parts := strings.Split(raw, ".")
	if len(parts) > 2 {
		return 0, false
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, false
	}
	frac := "0000"
	if len(parts) == 2 {
		if len(parts[1]) > 4 || parts[1] == "" {
			return 0, false
		}
		frac = (parts[1] + "0000")[:4]
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, false
	}
	v := whole*10000 + f
	if neg {
		v = -v
	}
	return v, true
}

func feeString(v int64) string {
	return strconv.FormatInt(v/10000, 10) + "." + fmt.Sprintf("%02d", (v%10000)/100)
}

func E4ToUSD(v int64) string {
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	return sign + strconv.FormatInt(v/10000, 10) + "." + pad4(v%10000)
}

func pad4(v int64) string {
	s := strconv.FormatInt(v, 10)
	for len(s) < 4 {
		s = "0" + s
	}
	return s
}

// ShouldRelease 只有设计里写明的情况可以把码放回未使用。
func ShouldRelease(upstream string, paymentAttempted, fundingDispatched bool, canRetry *bool) bool {
	return xlogic.ShouldRelease(upstream, paymentAttempted, fundingDispatched, canRetry)
}

func newToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// NewSiteCode 生成本站码 DNX-8-8-8。
func NewSiteCode() string {
	h := newToken(12)
	return "DNX-" + strings.ToUpper(h[0:8]) + "-" + strings.ToUpper(h[8:16]) + "-" + strings.ToUpper(h[16:24])
}

func NewDeviceToken() string { return newToken(32) }

func NewID() string { return newToken(16) }

// NewUUID 给上游的 clientRequestId 和 Idempotency-Key。文档要求 UUID。
func NewUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// CustomerView 把内部状态收成客户能看懂的阶段。
func CustomerView(status string) (step string, reusable bool, headline string) {
	switch status {
	case "unused":
		return "code", true, ""
	case "quoted":
		return "confirm", false, "请确认开通账号"
	case "funding", "funded", "paying", "processing":
		return "progress", false, "正在开通"
	case "paid_pending_delivery":
		return "progress", false, "已付款，等待到账"
	case "completed":
		return "done", false, "开通成功"
	case "review_required", "requires_action", "uncertain":
		return "locked", false, "正在核实付款结果"
	case "disabled":
		return "dead", false, "卡密已失效"
	default:
		return "progress", false, "正在处理"
	}
}
