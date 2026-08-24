//go:build epaylive

package epay

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestPayqixiangMapiLive(t *testing.T) {
	cfg := Config{
		APIBase:  "https://api.payqixiang.cn",
		PID:      "1003",
		Key:      "fnv5Xf0BnV5n5bGzFf7V7Fvn9tVtzn9v",
		SignMode: "append",
	}
	outTradeNo := "TEST" + strings.ReplaceAll(MoneyYuan(1), ".", "")
	params := map[string]string{
		"pid":          cfg.PID,
		"type":         "alipay",
		"out_trade_no": outTradeNo,
		"notify_url":   "http://example.com/notify",
		"return_url":   "http://example.com/return",
		"name":         "VIP",
		"money":        "0.01",
		"clientip":     "127.0.0.1",
		"device":       "jump",
	}
	params["sign"] = SignWithMode(params, cfg.Key, "append")
	params["sign_type"] = "MD5"
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	resp, err := http.Post(cfg.APIBase+"/mapi.php", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	t.Logf("mapi response: %s", body)
	if !strings.Contains(string(body), `"code":1`) && !strings.Contains(string(body), `"code": 1`) {
		t.Fatalf("mapi failed: %s", body)
	}
}

func TestPayqixiangSubmitLive(t *testing.T) {
	cfg := Config{
		APIBase:  "https://api.payqixiang.cn",
		PID:      "1003",
		Key:      "fnv5Xf0BnV5n5bGzFf7V7Fvn9tVtzn9v",
		SignMode: "append",
	}
	payURL := cfg.BuildPayURL("TESTSUBMIT001", "VIP", "0.01", "http://example.com/notify", "http://example.com/return", "alipay")
	resp, err := http.Get(payURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	t.Logf("submit GET response snippet: %s", body)
	if strings.Contains(string(body), "签名校验失败") {
		t.Fatalf("submit GET sign failed")
	}
}

func TestPayqixiangSubmitReturnURLWithQuery(t *testing.T) {
	cfg := Config{
		APIBase:  "https://api.payqixiang.cn",
		PID:      "1003",
		Key:      "fnv5Xf0BnV5n5bGzFf7V7Fvn9tVtzn9v",
		SignMode: "append",
	}
	returnURL := "https://example.com/return?paid=1&order_no=TESTSUBMIT002"
	payURL := cfg.BuildPayURL("TESTSUBMIT002", "VIP", "0.01", "http://example.com/notify", returnURL, "alipay")
	resp, err := http.Get(payURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	t.Logf("submit GET with query return_url: %s", body)
	if strings.Contains(string(body), "签名校验失败") {
		t.Fatalf("submit GET sign failed with query in return_url")
	}
}
