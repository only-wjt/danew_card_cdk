package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/gin-gonic/gin"
)

// 公开 Session 校验：用主台 API Key 调卡台凭据预检（不需要卡密），只回邮箱、当前套餐和结论。
//
// 卡台预检额度按本站主台账号算（60 次/分钟/用户、120 次/分钟/IP），真实兑换的预检也从本站出口走。
// 所以这里除了按访客 IP 限流，还要有全站上限和并发上限，结果再缓存一会儿，
// 不能让公开页把正常兑换的额度挤掉。
var (
	precheckIPMinuteLimiter = newIPLimiter(10, time.Minute)
	precheckIPHourLimiter   = newIPLimiter(60, time.Hour)
	// 全站共用一个 key；管理员也受这条约束。
	precheckGlobalLimiter = newIPLimiter(30, time.Minute)
	precheckSlots         = make(chan struct{}, 3)
	precheckResults       = newPrecheckCache(3*time.Minute, 2000)
	// 同一 IP 同一时间只放一条：公开页只支持逐条校验，并发脚本在这里被挡住
	precheckInFlight = newInFlightSet()
)

const (
	precheckSlotWait        = 10 * time.Second
	precheckUpstreamTimeout = 40 * time.Second
	precheckRetryAfter      = 30 * time.Second
	precheckBusyRetry       = 3 * time.Second
)

// inFlightSet 记录正在处理中的 key（这里是访客 IP）。
type inFlightSet struct {
	mu   sync.Mutex
	keys map[string]struct{}
}

func newInFlightSet() *inFlightSet {
	return &inFlightSet{keys: make(map[string]struct{})}
}

func (s *inFlightSet) acquire(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, busy := s.keys[key]; busy {
		return false
	}
	s.keys[key] = struct{}{}
	return true
}

func (s *inFlightSet) release(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.keys, key)
}

// precheckUpstream 测试可替换。返回 errPrecheckUnavailable 表示本站没有可用的卡台。
var precheckUpstream = func(ctx context.Context, session string) (*cardplatform.DirectPreflightResult, error) {
	acc, err := db.PrimaryCardPlatformAccount()
	if err != nil || !strings.EqualFold(acc.Status, "active") {
		return nil, errPrecheckUnavailable
	}
	// 凭据预检是 ZovoCard /openapi/v1 的接口，Avanfinity 没有。
	if p := strings.TrimSpace(acc.Protocol); p != "" && p != db.AccountProtocolSpaceXLegacy {
		return nil, errPrecheckUnavailable
	}
	return cardplatform.NewFromAccount(acc).DirectPreflight(ctx, session)
}

var errPrecheckUnavailable = errors.New("session precheck unavailable")

type precheckResponse struct {
	Valid       bool   `json:"valid"`
	ReasonCode  string `json:"reason_code,omitempty"`
	Message     string `json:"message,omitempty"`
	Email       string `json:"email,omitempty"`
	CurrentPlan string `json:"current_plan,omitempty"`
	Cached      bool   `json:"cached,omitempty"`
}

// PublicSessionPrecheck POST /api/v1/public/session/precheck
// body: {"session": "<完整 Session JSON 或 sessionToken>"}。不落库、不记 session。
func PublicSessionPrecheck(c *gin.Context) {
	var req struct {
		Session string `json:"session"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	raw := strings.TrimSpace(req.Session)
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请粘贴 Session"})
		return
	}
	if len(raw) > maxRawSessionBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Session 过长，请只粘贴一条"})
		return
	}
	// 新版 Session 带大段营销数据（marketingEligibility），精简后再按 16 KB 判断。
	raw = slimSessionJSON(raw)
	if len(raw) > maxBoundSessionBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Session 过长，请只粘贴一条"})
		return
	}
	// 与兑换同一条规则：只收 session 模式，裸 accessToken 或缺 sessionToken 的 JSON 兑换时也会被拒。
	session := extractCredentialSession(map[string]any{"session": raw})
	if session == "" {
		c.JSON(http.StatusOK, precheckResponse{
			ReasonCode: "SESSION_TOKEN_MISSING",
			Message:    "缺少 sessionToken：兑换需要完整 Session JSON（含 sessionToken），只有 accessToken 不行",
		})
		return
	}

	key := precheckKey(session)
	if hit, ok := precheckResults.get(key); ok {
		hit.Cached = true
		c.JSON(http.StatusOK, hit)
		return
	}

	if !requestIsAdmin(c) {
		ip := c.ClientIP()
		// 先占位再计次：被并发挡回的请求不消耗额度
		if !precheckInFlight.acquire(ip) {
			rejectRateLimited(c, precheckBusyRetry, "上一条还在校验中，请等它完成后再校验下一条")
			return
		}
		defer precheckInFlight.release(ip)
		if ok, wait := precheckIPMinuteLimiter.allow(ip, 1); !ok {
			rejectRateLimited(c, wait, "校验太频繁，请稍后再试")
			return
		}
		if ok, wait := precheckIPHourLimiter.allow(ip, 1); !ok {
			rejectRateLimited(c, wait, "本小时校验次数已用完，请稍后再试")
			return
		}
	}
	if ok, wait := precheckGlobalLimiter.allow("global", 1); !ok {
		rejectRateLimited(c, wait, "校验人数较多，请稍后再试")
		return
	}

	select {
	case precheckSlots <- struct{}{}:
		defer func() { <-precheckSlots }()
	case <-time.After(precheckSlotWait):
		rejectRateLimited(c, precheckRetryAfter, "校验人数较多，请稍后再试")
		return
	case <-c.Request.Context().Done():
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), precheckUpstreamTimeout)
	defer cancel()
	res, err := precheckUpstream(ctx, session)
	if err == nil {
		out := precheckResponse{Valid: true, Email: res.Email, CurrentPlan: res.CurrentPlan}
		precheckResults.put(key, out)
		c.JSON(http.StatusOK, out)
		return
	}

	status, out := precheckOutcome(err)
	switch status {
	case http.StatusOK:
		precheckResults.put(key, out)
		c.JSON(http.StatusOK, out)
	case http.StatusTooManyRequests:
		rejectRateLimited(c, precheckRetryAfter, out.Message)
	default:
		if !errors.Is(err, errPrecheckUnavailable) {
			log.Printf("[session-precheck] upstream error: %v", err)
		}
		c.JSON(status, gin.H{"error": out.Message})
	}
}

// precheckOutcome 把卡台错误分成两类：凭据本身的结论（200，可缓存）和服务问题（非 200，不缓存）。
func precheckOutcome(err error) (int, precheckResponse) {
	unavailable := precheckResponse{Message: "Session 校验暂不可用，请稍后再试"}
	var apiErr *cardplatform.APIError
	if !errors.As(err, &apiErr) {
		// 网络错误、超时、本站没配卡台
		return http.StatusServiceUnavailable, unavailable
	}
	code := strings.ToUpper(strings.TrimSpace(apiErr.ErrorCode))
	switch {
	case code == "GPT_PREFLIGHT_RATE_LIMITED" || apiErr.HTTPStatus == http.StatusTooManyRequests:
		return http.StatusTooManyRequests, precheckResponse{Message: "校验人数较多，请稍后再试"}
	case code == "GPT_SESSION_INVALID":
		return http.StatusOK, precheckResponse{ReasonCode: code, Message: "Session 无效或已过期，请重新打开 chatgpt.com/api/auth/session 复制"}
	case code == "PRECHECK_REJECTED":
		return http.StatusOK, precheckResponse{ReasonCode: code, Message: joinReason("账号暂不满足兑换条件", apiErr.Msg)}
	case apiErr.HTTPStatus == http.StatusUnauthorized || apiErr.HTTPStatus == http.StatusForbidden,
		apiErr.HTTPStatus >= 500,
		strings.HasPrefix(code, "GPT_DIRECT_"),
		code == "INSUFFICIENT_BALANCE", code == "INVALID_REQUEST", code == "DIRECT_PRODUCT_DISABLED":
		// 本站 Key、白名单、直充权限、余额或卡台自身的问题，不是用户 Session 的问题
		return http.StatusServiceUnavailable, unavailable
	case apiErr.HTTPStatus >= 400:
		// 未知错误码不透传给访客，只给通用结论；原文在日志里
		log.Printf("[session-precheck] rejected: %v", apiErr)
		return http.StatusOK, precheckResponse{ReasonCode: "PRECHECK_FAILED", Message: joinReason("Session 校验未通过", apiErr.Msg)}
	}
	return http.StatusServiceUnavailable, unavailable
}

// 上游原文里带这些词的不给访客看：公开页面不能暴露本站接的是哪家、用的什么账户。
var precheckHiddenTerms = []string{
	"卡台", "卡片", "开卡", "平台", "余额", "服务费", "直充", "密钥", "白名单", "商户", "代理",
	"zovo", "spacex", "avanfinity", "card", "balance", "api", "key", "merchant", "platform",
}

// joinReason 上游原文只在「不暴露本站接入信息」时附上，否则只给前缀。
func joinReason(prefix, msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" || msg == "business error" {
		return prefix
	}
	// 上游这段话是给下单写的（「…后重试，未扣款」），校验页不扣款，这半句只会让人以为要扣钱
	for _, tail := range []string{"，未扣款", "。未扣款", ",未扣款", "未扣款"} {
		msg = strings.TrimSpace(strings.TrimSuffix(msg, tail))
	}
	lower := strings.ToLower(msg)
	for _, term := range precheckHiddenTerms {
		if strings.Contains(lower, term) {
			return prefix
		}
	}
	if r := []rune(msg); len(r) > 120 {
		msg = string(r[:120]) + "…"
	}
	return prefix + "：" + msg
}

func precheckKey(session string) string {
	sum := sha256.Sum256([]byte(session))
	return hex.EncodeToString(sum[:])
}

// precheckCache 只存结论（不存 session），key 是 session 的 sha256。
type precheckCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	maxKeys int
	items   map[string]precheckEntry
}

type precheckEntry struct {
	at  time.Time
	res precheckResponse
}

func newPrecheckCache(ttl time.Duration, maxKeys int) *precheckCache {
	return &precheckCache{ttl: ttl, maxKeys: maxKeys, items: make(map[string]precheckEntry)}
}

func (p *precheckCache) get(key string) (precheckResponse, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.items[key]
	if !ok || time.Since(e.at) > p.ttl {
		return precheckResponse{}, false
	}
	return e.res, true
}

func (p *precheckCache) put(key string, res precheckResponse) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.items) >= p.maxKeys {
		for k, e := range p.items {
			if time.Since(e.at) > p.ttl {
				delete(p.items, k)
			}
		}
		if len(p.items) >= p.maxKeys {
			// 全是新鲜条目时直接清空：缓存只是省额度，丢了无妨
			p.items = make(map[string]precheckEntry)
		}
	}
	res.Cached = false
	p.items[key] = precheckEntry{at: time.Now(), res: res}
}

func (p *precheckCache) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.items = make(map[string]precheckEntry)
}
