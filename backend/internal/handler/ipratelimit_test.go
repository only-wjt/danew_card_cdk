package handler

import (
	"fmt"
	"testing"
	"time"
)

// resetLoginAttempts 清空全局登录失败表，隔离各测试。
func resetLoginAttempts() {
	loginMu.Lock()
	loginAttempts = make(map[string]*loginAttempt)
	loginLastGC = time.Time{}
	loginMu.Unlock()
}

// 单用户名维度：同一 (IP+用户名) 连续失败达到阈值即锁定（既有行为不回退）。
func TestLoginPerUserLocks(t *testing.T) {
	resetLoginAttempts()
	ip := "9.9.9.1"
	ipKey := loginIPKey(ip)
	userKey := loginRateKey(ip, "admin")
	for i := 0; i < loginMaxAttempts; i++ {
		if ok, _ := loginCheck(ipKey, userKey); !ok {
			t.Fatalf("attempt %d should be allowed", i)
		}
		loginRecordFailure(ipKey, userKey)
	}
	if ok, wait := loginCheck(ipKey, userKey); ok || wait <= 0 {
		t.Fatalf("same username should be locked after %d failures", loginMaxAttempts)
	}
}

// IP 维度：单 IP 不断更换用户名（每个用户名只失败一次，永不触发单用户名闸），
// 累计失败到 loginIPMaxAttempts 时，该 IP 即便换一个全新用户名也应被整体锁定。
// 这是渗透测试里「54 次换用户名未被拦」的回归防护。
func TestLoginPerIPBlocksUsernameSpray(t *testing.T) {
	resetLoginAttempts()
	ip := "9.9.9.2"
	ipKey := loginIPKey(ip)
	for i := 0; i < loginIPMaxAttempts; i++ {
		userKey := loginRateKey(ip, fmt.Sprintf("spray-user-%d", i))
		if ok, _ := loginCheck(ipKey, userKey); !ok {
			t.Fatalf("spray attempt %d should still be allowed (per-user bucket not tripped)", i)
		}
		loginRecordFailure(ipKey, userKey)
	}
	// 一个从未出现过的新用户名——per-用户名桶为空，只能被 per-IP 闸拦住。
	fresh := loginRateKey(ip, "brand-new-name")
	if ok, wait := loginCheck(ipKey, fresh); ok || wait <= 0 {
		t.Fatalf("new username from sprayed IP must be blocked by per-IP gate, wait=%v", wait)
	}
	// 另一个 IP 不受影响。
	other := "9.9.9.3"
	if ok, _ := loginCheck(loginIPKey(other), loginRateKey(other, "brand-new-name")); !ok {
		t.Fatal("a different IP must be independent")
	}
}

func TestIPLimiterWeighted(t *testing.T) {
	l := newIPLimiter(200, 10*time.Minute)
	if ok, _ := l.allow("1.1.1.1", 100); !ok {
		t.Fatal("first 100 should pass")
	}
	if ok, _ := l.allow("1.1.1.1", 100); !ok {
		t.Fatal("second 100 should pass")
	}
	ok, wait := l.allow("1.1.1.1", 1)
	if ok || wait <= 0 {
		t.Fatalf("201st should be limited, wait=%v", wait)
	}
	if ok, _ := l.allow("2.2.2.2", 100); !ok {
		t.Fatal("other ip independent")
	}
}

func TestIPLimiterWindowExpiry(t *testing.T) {
	l := newIPLimiter(1, 20*time.Millisecond)
	if ok, _ := l.allow("k", 1); !ok {
		t.Fatal("first")
	}
	if ok, _ := l.allow("k", 1); ok {
		t.Fatal("second within window")
	}
	time.Sleep(30 * time.Millisecond)
	if ok, _ := l.allow("k", 1); !ok {
		t.Fatal("after window")
	}
}

func TestIPLimiterBoundedKeys(t *testing.T) {
	l := newIPLimiter(5, time.Minute)
	l.maxKeys = 3
	for _, k := range []string{"a", "b", "c"} {
		if ok, _ := l.allow(k, 1); !ok {
			t.Fatalf("%s should pass", k)
		}
	}
	if ok, _ := l.allow("d", 1); ok {
		t.Fatal("new key beyond cap should be refused")
	}
	if ok, _ := l.allow("a", 1); !ok {
		t.Fatal("existing key still counted")
	}
	if len(l.hits) > 3 {
		t.Fatalf("map grew to %d", len(l.hits))
	}
}

func TestLoginRateKeyTruncates(t *testing.T) {
	long := make([]byte, 100000)
	for i := range long {
		long[i] = 'a'
	}
	if k := loginRateKey("1.2.3.4", string(long)); len(k) > 80 {
		t.Fatalf("key len %d", len(k))
	}
}

func TestCredentialIsMailbox(t *testing.T) {
	cases := []struct {
		cred any
		want bool
	}{
		{map[string]any{"mode": "mailbox", "email": "a@b.c"}, true},
		{map[string]any{"mode": "MAILBOX"}, true},
		{map[string]any{"mode": "session", "password": "x"}, true},
		{map[string]any{"mode": "session", "session": "{}"}, false},
		{nil, false},
	}
	for i, c := range cases {
		if got := credentialIsMailbox(c.cred); got != c.want {
			t.Errorf("case %d: got %v", i, got)
		}
	}
}

func TestBindableSessionRequires2xx(t *testing.T) {
	body := map[string]any{"credential": map[string]any{"mode": "session", "session": `{"sessionToken":"t"}`}}
	if s := bindableSession(400, body); s != "" {
		t.Fatal("non-2xx must not bind")
	}
	if s := bindableSession(200, body); s == "" {
		t.Fatal("2xx should bind")
	}
}
