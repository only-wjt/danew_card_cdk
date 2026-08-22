package agentwebhook

import (
	"encoding/hex"
	"strings"
	"testing"
)

// 回调地址由代理自由填写，必须挡住指向内网/保留地址的目标，
// 否则等于给代理开了一个探内网和云元数据接口的口子。
func TestValidateURLBlocksInternalTargets(t *testing.T) {
	blocked := []string{
		"http://127.0.0.1/hook",
		"http://localhost/hook",
		"https://10.0.0.5/hook",
		"http://192.168.1.10:8080/hook",
		"http://172.16.3.4/hook",
		"http://169.254.169.254/latest/meta-data/", // 云厂商元数据
		"http://[::1]/hook",
		"http://0.0.0.0/hook",
		"http://100.64.1.1/hook", // CGNAT
		"ftp://example.com/hook",  // 协议不支持
		"http://[fd00::1]/hook",   // IPv6 ULA
		"http://198.18.0.242/cb",  // 基准测试保留段，也是常见 fake-ip 段
		"http://192.0.0.1/hook",   // IETF 协议分配段
		"http://224.0.0.1/hook",   // 组播
		"https://[fe80::1]/hook",  // IPv6 链路本地
	}
	for _, raw := range blocked {
		if err := ValidateURL(raw); err == nil {
			t.Errorf("ValidateURL(%q) = nil, 该地址必须被拒绝", raw)
		}
	}
}

// 公网地址必须放行，否则代理根本配不上回调。
// 用 IP 字面量而不是域名：本机若挂了 fake-ip 代理，域名会被解析到
// 198.18.0.0/15，测试就会因环境而不是因代码失败。
func TestValidateURLAllowsPublicTargets(t *testing.T) {
	allowed := []string{
		"https://93.184.215.14/hook",
		"https://8.8.8.8:8443/callback",
		"http://1.1.1.1/cb?x=1",
		"https://[2606:4700:4700::1111]/hook",
	}
	for _, raw := range allowed {
		if err := ValidateURL(raw); err != nil {
			t.Errorf("ValidateURL(%q) = %v, 公网地址应放行", raw, err)
		}
	}
}

// 留空表示不启用回调，不应报错。
func TestValidateURLAllowsEmpty(t *testing.T) {
	if err := ValidateURL("   "); err != nil {
		t.Fatalf("空地址应视为不启用回调, got %v", err)
	}
}

// 签名口径必须是 hex(HMAC-SHA256(secret, timestamp + "." + body))，
// 文档里给代理的验签示例就是照这个写的，改了会让所有代理验签失败。
func TestSignMatchesDocumentedScheme(t *testing.T) {
	got := Sign("s3cr3t", "1700000000", []byte(`{"event_id":"evt_1"}`))
	if len(got) != 64 {
		t.Fatalf("签名应为 64 位 hex, got %d 位: %q", len(got), got)
	}
	if _, err := hex.DecodeString(got); err != nil {
		t.Fatalf("签名不是合法 hex: %v", err)
	}
	if strings.ToLower(got) != got {
		t.Fatalf("签名应为小写 hex: %q", got)
	}

	// 时间戳参与签名：换个时间戳必须得到不同结果，否则重放防护是假的
	other := Sign("s3cr3t", "1700000001", []byte(`{"event_id":"evt_1"}`))
	if other == got {
		t.Fatal("时间戳未参与签名，重放防护失效")
	}
	// body 参与签名
	if Sign("s3cr3t", "1700000000", []byte(`{"event_id":"evt_2"}`)) == got {
		t.Fatal("body 未参与签名")
	}
	// 密钥参与签名
	if Sign("other", "1700000000", []byte(`{"event_id":"evt_1"}`)) == got {
		t.Fatal("密钥未参与签名")
	}
}

// 退避序列必须够用：最后一次重试前的等待次数不能少于 maxAttempts - 1，
// 否则 retryOrFail 会越界或提前放弃。
func TestBackoffCoversAllAttempts(t *testing.T) {
	if len(backoffSec) < maxAttempts-1 {
		t.Fatalf("backoffSec 有 %d 项，maxAttempts=%d，重试排期不完整", len(backoffSec), maxAttempts)
	}
	for i := 1; i < len(backoffSec); i++ {
		if backoffSec[i] <= backoffSec[i-1] {
			t.Fatalf("退避应递增: %v", backoffSec)
		}
	}
}
