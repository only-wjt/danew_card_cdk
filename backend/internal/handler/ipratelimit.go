package handler

import (
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danew/cdk-recharge-system/internal/auth"
	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/gin-gonic/gin"
)

// ipLimiter 按 key（通常是客户端 IP）做滑动窗口限流，支持按权重计数（批量查卡按张数）。
// 过期 key 由后台定期清理；key 总数有上限，防止伪造大量来源把内存打爆。
type ipLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	limit   int
	maxKeys int
	hits    map[string][]limiterHit
	lastGC  time.Time
}

type limiterHit struct {
	at     time.Time
	weight int
}

func newIPLimiter(limit int, window time.Duration) *ipLimiter {
	return &ipLimiter{
		window:  window,
		limit:   limit,
		maxKeys: 100000,
		hits:    make(map[string][]limiterHit),
	}
}

// allow 尝试记入 weight 次；超限时返回 false 及建议等待时长。
func (l *ipLimiter) allow(key string, weight int) (bool, time.Duration) {
	if weight <= 0 {
		weight = 1
	}
	now := time.Now()
	cut := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastGC) > l.window || len(l.hits) >= l.maxKeys {
		l.gcLocked(cut)
		l.lastGC = now
	}

	kept := l.hits[key][:0]
	used := 0
	for _, h := range l.hits[key] {
		if h.at.After(cut) {
			kept = append(kept, h)
			used += h.weight
		}
	}
	if used+weight > l.limit {
		if len(kept) == 0 {
			delete(l.hits, key)
			return false, l.window
		}
		l.hits[key] = kept
		return false, kept[0].at.Add(l.window).Sub(now)
	}
	if len(kept) == 0 && len(l.hits) >= l.maxKeys {
		// 表满：宁可拒绝新来源，也不让内存无限增长
		return false, l.window
	}
	l.hits[key] = append(kept, limiterHit{at: now, weight: weight})
	return true, 0
}

func (l *ipLimiter) gcLocked(cut time.Time) {
	for k, hs := range l.hits {
		if len(hs) == 0 || !hs[len(hs)-1].at.After(cut) {
			delete(l.hits, k)
		}
	}
}

func (l *ipLimiter) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hits = make(map[string][]limiterHit)
}

var (
	// 公开查卡：单查 / 批量（批量按张数计）
	lookupSingleLimiter = newIPLimiter(30, time.Minute)
	lookupBatchLimiter  = newIPLimiter(200, 10*time.Minute)
	// 公开兑换 preview：同样能核对卡密状态且回源卡台。
	// 额度按公开批量兑换上限 1000 张 × 每张 2 次 preview 留余量，正常批量不受影响。
	publicCDKPreviewLimiter = newIPLimiter(2500, 10*time.Minute)
	// 凭卡密查进度（单张兑换页轮询约 20 次/分钟）
	resultByCodeLimiter = newIPLimiter(60, time.Minute)
	// 账单 / session 工具
	billingLimiter = newIPLimiter(10, time.Minute)
	// X / TG 公开兑换
	xPublicLimiter  = newIPLimiter(30, time.Minute)
	tgPublicLimiter = newIPLimiter(30, time.Minute)
	// 代理换码
	swapLimiter = newIPLimiter(20, time.Hour)
)

// rejectRateLimited 统一 429 响应。
func rejectRateLimited(c *gin.Context, wait time.Duration, msg string) {
	secs := int(math.Ceil(wait.Seconds()))
	if secs < 1 {
		secs = 1
	}
	c.Header("Retry-After", strconv.Itoa(secs))
	if msg == "" {
		msg = "请求过于频繁，请稍后再试"
	}
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": msg, "retry_after": secs})
}

// RateLimitByIP 路由级中间件：管理员登录态不受限。
func RateLimitByIP(l *ipLimiter, msg string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if requestIsAdmin(c) {
			c.Next()
			return
		}
		if ok, wait := l.allow(c.ClientIP(), 1); !ok {
			rejectRateLimited(c, wait, msg)
			return
		}
		c.Next()
	}
}

const ctxKeyIsAdmin = "lookup_is_admin"

// requestIsAdmin 识别公开接口上的管理员登录态（Bearer 或 auth_token cookie），
// 仅用于放宽限流 / 展示完整信息，不替代 JWTAuthMiddleware。
func requestIsAdmin(c *gin.Context) bool {
	if v, ok := c.Get(ctxKeyIsAdmin); ok {
		b, _ := v.(bool)
		return b
	}
	is := detectAdmin(c)
	c.Set(ctxKeyIsAdmin, is)
	return is
}

func detectAdmin(c *gin.Context) bool {
	tok := ""
	if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if tok == "" || strings.HasPrefix(tok, "ak_live_") {
		if ck, err := c.Cookie(authCookieName); err == nil {
			tok = ck
		}
	}
	if tok == "" {
		return false
	}
	claims, err := auth.ParseToken(tok)
	if err != nil || !claims.IsAdmin || claims.IsAgent {
		return false
	}
	return db.AdminUserActive(claims.UserID)
}

func RateLimitLookupSingle() gin.HandlerFunc {
	return RateLimitByIP(lookupSingleLimiter, "查询太频繁，请稍后再试")
}

func RateLimitPublicCDKPreview() gin.HandlerFunc {
	return RateLimitByIP(publicCDKPreviewLimiter, "请求过于频繁，请稍后再试")
}

func RateLimitResultByCode() gin.HandlerFunc {
	return RateLimitByIP(resultByCodeLimiter, "查询太频繁，请稍后再试")
}

func RateLimitBilling() gin.HandlerFunc {
	return RateLimitByIP(billingLimiter, "查询太频繁，请稍后再试")
}

// writeUpstreamUnavailable 上游网络错误不透传给公开接口（会带出卡台地址/IP），只记日志。
func writeUpstreamUnavailable(c *gin.Context, tag string, err error) {
	log.Printf("[%s] upstream error: %v", tag, err)
	c.JSON(http.StatusBadGateway, gin.H{"error": "兑换服务暂时不可用，请稍后重试"})
}
