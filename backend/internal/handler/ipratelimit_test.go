package handler

import (
	"testing"
	"time"
)

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
