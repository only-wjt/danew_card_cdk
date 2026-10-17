package handler

import (
	"strings"
	"sync"
	"time"
)

// 简单的内存级登录限流：同一 key（客户端IP+用户名）在窗口期内失败超过阈值即锁定。
const (
	loginMaxAttempts = 5
	loginLockWindow  = 15 * time.Minute
	// loginIPMaxAttempts 是单 IP 跨「所有用户名」的聚合失败上限。
	// per-(IP+用户名) 的闸挡不住用户名喷洒（每换一个用户名就是一个新桶），
	// 这道 IP 维度的闸保证单来源不管怎么轮换用户名，累计失败到阈值即整体锁定。
	// 阈值取得比单用户名闸高，给合法用户（偶尔记错密码 / NAT 后多人）留余量，但远低于喷洒规模。
	loginIPMaxAttempts = 20
)

type loginAttempt struct {
	failures    int
	lockedUntil time.Time
	windowStart time.Time
}

var (
	loginMu       sync.Mutex
	loginAttempts = make(map[string]*loginAttempt)
	loginLastGC   time.Time
)

// 失败记录上限：超过后先清理过期项，仍满则不再新增（已有 key 照常计数）。
const loginMaxKeys = 50000

// loginRateKey 组装限流 key；用户名截断，防止超长用户名撑大内存。
func loginRateKey(ip, username string) string {
	u := strings.ToLower(strings.TrimSpace(username))
	if len(u) > 64 {
		u = u[:64]
	}
	return ip + "|" + u
}

// loginIPKey 是单 IP 维度（跨用户名）的限流 key。
// 用 "ip:" 前缀与 loginRateKey 的 "ip|username" 键空间隔离——IP 不含竖线，
// 二者的键集合不会相交，共用同一张 loginAttempts 表而互不干扰。
func loginIPKey(ip string) string {
	return "ip:" + ip
}

func loginGCLocked(now time.Time) {
	for k, a := range loginAttempts {
		if now.After(a.lockedUntil) && now.Sub(a.windowStart) > loginLockWindow {
			delete(loginAttempts, k)
		}
	}
	loginLastGC = now
}

// loginAllowed 返回是否允许尝试登录，以及若被锁定还需等待多久。
func loginAllowed(key string) (bool, time.Duration) {
	loginMu.Lock()
	defer loginMu.Unlock()

	a := loginAttempts[key]
	if a == nil {
		return true, 0
	}
	now := time.Now()
	if now.Before(a.lockedUntil) {
		return false, time.Until(a.lockedUntil)
	}
	// 锁已过期 / 窗口已过：重置
	if !a.lockedUntil.IsZero() && now.After(a.lockedUntil) {
		delete(loginAttempts, key)
	}
	return true, 0
}

// loginFailed 记一次 per-(IP+用户名) 维度的失败，达到阈值则锁定。
func loginFailed(key string) {
	loginFailedN(key, loginMaxAttempts)
}

// loginFailedN 记一次失败，达到 maxAttempts 则锁定该 key。
// 供 per-(IP+用户名) 与 per-IP 两个维度复用不同阈值。
func loginFailedN(key string, maxAttempts int) {
	loginMu.Lock()
	defer loginMu.Unlock()

	now := time.Now()
	if now.Sub(loginLastGC) > time.Minute || len(loginAttempts) >= loginMaxKeys {
		loginGCLocked(now)
	}
	a := loginAttempts[key]
	if a == nil || now.Sub(a.windowStart) > loginLockWindow {
		if a == nil && len(loginAttempts) >= loginMaxKeys {
			return
		}
		a = &loginAttempt{windowStart: now}
		loginAttempts[key] = a
	}
	a.failures++
	if a.failures >= maxAttempts {
		a.lockedUntil = now.Add(loginLockWindow)
	}
}

// loginSucceeded 登录成功后清除该 key 的失败计数。
func loginSucceeded(key string) {
	loginMu.Lock()
	defer loginMu.Unlock()
	delete(loginAttempts, key)
}

// loginCheck 在处理登录请求前调用：先过 per-IP 聚合闸（挡用户名喷洒），
// 再过 per-(IP+用户名) 闸。任一维度被锁即返回 false + 建议等待时长。
func loginCheck(ipKey, userKey string) (bool, time.Duration) {
	if ok, wait := loginAllowed(ipKey); !ok {
		return false, wait
	}
	return loginAllowed(userKey)
}

// loginRecordFailure 一次认证失败同时记入两个维度：
// per-(IP+用户名) 用标准阈值，per-IP 用更高的聚合阈值。
// 注意：成功登录只清 per-(IP+用户名) 计数，不清 per-IP 聚合计数——
// 这样攻击者即便在喷洒中偶然命中一个弱口令，也无法借此重置整段 IP 失败账，
// 喷洒仍会累计触发 IP 锁。per-IP 计数靠窗口自然老化。
func loginRecordFailure(ipKey, userKey string) {
	loginFailed(userKey)
	loginFailedN(ipKey, loginIPMaxAttempts)
}
