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

// loginFailed 记一次失败，达到阈值则锁定。
func loginFailed(key string) {
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
	if a.failures >= loginMaxAttempts {
		a.lockedUntil = now.Add(loginLockWindow)
	}
}

// loginSucceeded 登录成功后清除该 key 的失败计数。
func loginSucceeded(key string) {
	loginMu.Lock()
	defer loginMu.Unlock()
	delete(loginAttempts, key)
}
