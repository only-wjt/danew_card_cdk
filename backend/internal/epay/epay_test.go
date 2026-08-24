package epay

import "testing"

func TestSignAndVerify(t *testing.T) {
	key := "test-secret-key"
	params := map[string]string{
		"pid":          "1001",
		"type":         "alipay",
		"out_trade_no": "AG1234567890abcd",
		"notify_url":   "https://example.com/notify",
		"return_url":   "https://example.com/return",
		"name":         "CDK Plus x1",
		"money":        "29.99",
	}
	params["sign"] = Sign(params, key)
	params["sign_type"] = "MD5"
	cfg := Config{Key: key, SignMode: "append"}
	ok, msg := cfg.VerifyNotify(params)
	if !ok {
		t.Fatalf("verify failed: %s sign=%s", msg, params["sign"])
	}
}

func TestSignPayqixiangAppendMode(t *testing.T) {
	params := map[string]string{
		"money":        "0.01",
		"name":         "VIP",
		"notify_url":   "http://www.example.com/notify",
		"out_trade_no": "20160806151343349",
		"pid":          "1003",
		"return_url":   "http://www.example.com/return",
		"type":         "alipay",
	}
	key := "fnv5Xf0BnV5n5bGzFf7V7Fvn9tVtzn9v"
	if SignWithMode(params, key, "append") == SignWithMode(params, key, "key_param") {
		t.Fatal("append and key_param modes must differ")
	}
	params["sign"] = SignWithMode(params, key, "append")
	params["sign_type"] = "MD5"
	cfg := Config{Key: key, SignMode: "append"}
	ok, msg := cfg.VerifyNotify(params)
	if !ok {
		t.Fatalf("payqixiang verify: %s sign=%s", msg, params["sign"])
	}
}

func TestMoneyYuan(t *testing.T) {
	if MoneyYuan(2999) != "29.99" {
		t.Fatalf("2999 cents")
	}
	cents, err := ParseMoneyYuan("29.99")
	if err != nil || cents != 2999 {
		t.Fatalf("parse 29.99 = %d err=%v", cents, err)
	}
}

func TestParsePayTypes(t *testing.T) {
	if got := ParsePayTypes(""); len(got) != 1 || got[0] != "alipay" {
		t.Fatalf("default = %#v", got)
	}
	if got := ParsePayTypes("alipay,wxpay"); len(got) != 2 {
		t.Fatalf("both = %#v", got)
	}
	if !AllowedPayType("alipay", []string{"alipay"}) || AllowedPayType("wxpay", []string{"alipay"}) {
		t.Fatal("allowed check")
	}
}
