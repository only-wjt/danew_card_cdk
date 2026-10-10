package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danew/cdk-recharge-system/internal/db"
)

type avanCapture struct {
	path    string
	headers http.Header
	body    map[string]any
}

func newAvanServer(t *testing.T, reply func(path string) (int, string)) (*httptest.Server, *[]avanCapture) {
	t.Helper()
	var got []avanCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		got = append(got, avanCapture{path: r.URL.Path, headers: r.Header.Clone(), body: body})
		st, resp := reply(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(st)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func avanAccount(base string) db.CardPlatformAccount {
	return db.CardPlatformAccount{ID: 7, Protocol: ProtocolAvanfinity202608, SiteBase: base + "/api/v1/", CredPublic: "app_x", CredSecret: "sec_y"}
}

func TestAvanfinityV2026IssueCDK(t *testing.T) {
	srv, got := newAvanServer(t, func(string) (int, string) {
		return 200, `{"code":0,"data":{"saleId":"s1","cdkId":"c1","code":"AVF-0123456789ABCDEF0123456789ABCDEF","codePrefix":"AVF-01234567","plan":"plus","salePrice":"12.345","quantity":1,"totalSalePrice":"12.345","items":[],"balance":"1","replayed":false,"generated":true,"forceNewCard":false}}`
	})
	p := NewAvanfinityV2026(avanAccount(srv.URL))
	up, err := p.IssueCDK(context.Background(), "gpt_plus_1m", "short", IssuePreference{PaymentCountry: "US"})
	if err != nil {
		t.Fatal(err)
	}
	c := (*got)[0]
	if c.path != "/api/v1/gpt-direct/cdks/generate" {
		t.Fatalf("path %s", c.path)
	}
	if c.headers.Get("X-App-Id") != "app_x" || c.headers.Get("X-App-Secret") != "sec_y" {
		t.Fatal("missing app credentials")
	}
	if k := c.headers.Get("Idempotency-Key"); len(k) < 8 || k != up.Idempotency {
		t.Fatalf("idempotency key %q / %q", k, up.Idempotency)
	}
	if c.body["plan"] != "plus" || c.body["quantity"].(float64) != 1 {
		t.Fatalf("body %+v", c.body)
	}
	if _, ok := c.body["payment_country"]; ok {
		t.Fatal("payment country must not be sent")
	}
	if up.RemoteID != "c1" || up.RemoteCode != "AVF-0123456789ABCDEF0123456789ABCDEF" || up.CodePrefix != "AVF-01234567" || up.FeeMinor != 1235 {
		t.Fatalf("issued %+v", up)
	}
}

func TestAvanfinityV2026IssueNullCodeFails(t *testing.T) {
	srv, _ := newAvanServer(t, func(string) (int, string) {
		return 200, `{"code":0,"data":{"cdkId":"c1","code":null,"codePrefix":"AVF-********","plan":"plus","salePrice":"1.00","replayed":true}}`
	})
	if _, err := NewAvanfinityV2026(avanAccount(srv.URL)).IssueCDK(context.Background(), "plus", "idem-12345678", IssuePreference{}); err == nil {
		t.Fatal("expected error when code is null")
	}
}

func TestAvanfinityV2026ErrorCode(t *testing.T) {
	srv, _ := newAvanServer(t, func(string) (int, string) {
		return 409, `{"error":"余额不足","errorCode":"SPENDABLE_BALANCE_INSUFFICIENT"}`
	})
	_, err := NewAvanfinityV2026(avanAccount(srv.URL)).IssueCDK(context.Background(), "plus", "idem-12345678", IssuePreference{})
	apiErr, ok := err.(*AvanfinityAPIError)
	if !ok || apiErr.ErrorCode != "SPENDABLE_BALANCE_INSUFFICIENT" || apiErr.Status != 409 {
		t.Fatalf("err %#v", err)
	}
}

func TestAvanfinityV2026RedeemFlow(t *testing.T) {
	srv, got := newAvanServer(t, func(path string) (int, string) {
		if strings.HasSuffix(path, "/preview") {
			return 200, `{"code":0,"data":{"redemptionToken":"rt1","plan":"plus"}}`
		}
		if strings.HasSuffix(path, "/redeem") {
			return 202, `{"code":0,"data":{"status":"PROCESSING"}}`
		}
		return 200, `{"code":0,"data":{"preflightToken":"pf1"}}`
	})
	p := NewAvanfinityV2026(avanAccount(srv.URL))
	ctx := context.Background()

	_, raw, err := p.Preview(ctx, "AVF-X", "Mozilla/5.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"redemption_token":"rt1"`) {
		t.Fatalf("snake alias missing: %s", raw)
	}
	if _, _, err := p.Preflight(ctx, map[string]any{"redemption_token": "rt1", "credential": map[string]any{"mode": "session", "session": "s"}}, "Mozilla/5.0"); err != nil {
		t.Fatal(err)
	}
	st, _, err := p.Redeem(ctx, map[string]any{"redemption_token": "rt1", "preflight_token": "pf1", "client_request_id": "web-abc-1"}, "Mozilla/5.0")
	if err != nil || st != 202 {
		t.Fatalf("redeem st=%d err=%v", st, err)
	}

	dev := (*got)[0].headers.Get("X-Redemption-Device")
	if !uuidRe.MatchString(dev) {
		t.Fatalf("device not uuid: %q", dev)
	}
	for _, c := range *got {
		if c.headers.Get("X-Redemption-Device") != dev {
			t.Fatal("device must stay the same across steps")
		}
		if c.headers.Get("X-App-Secret") != "" {
			t.Fatal("public endpoints must not carry app secret")
		}
	}
	pf := (*got)[1].body
	if pf["redemptionToken"] != "rt1" || pf["redemption_token"] != nil {
		t.Fatalf("preflight body %+v", pf)
	}
	rd := (*got)[2].body
	if rd["redemptionToken"] != "rt1" || rd["preflightToken"] != "pf1" || !uuidRe.MatchString(rd["clientRequestId"].(string)) {
		t.Fatalf("redeem body %+v", rd)
	}
}

func TestAvanfinityV2026BalanceAndRefund(t *testing.T) {
	srv, got := newAvanServer(t, func(string) (int, string) {
		return 200, `{"code":0,"data":{"balance":"100.50","totalBalance":"120","riskLockedBalance":"10.25","retryReservedBalance":"5"}}`
	})
	p := NewAvanfinityV2026(avanAccount(srv.URL))
	b, err := p.(AvanfinityAccount).Balance(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b.Spendable != "85.25" || (*got)[0].path != "/api/v1/balance" {
		t.Fatalf("balance %+v path %s", b, (*got)[0].path)
	}
	if p.DeleteAndRefund(context.Background(), "c1") != ErrRefundUnsupported || p.Disable(context.Background(), "c1") != ErrRefundUnsupported {
		t.Fatal("reclaim must be unsupported")
	}
}

func TestAvanfinityRedeemClientRequestIDMatchesWire(t *testing.T) {
	for _, input := range []string{"web-abc-1", "  web-abc-1  ", "550E8400-E29B-41D4-A716-446655440000", ""} {
		t.Run(input, func(t *testing.T) {
			effective := AvanfinityRedeemClientRequestID(input)
			if !uuidRe.MatchString(effective) {
				t.Fatalf("effective=%q", effective)
			}
			if input != "" && effective != AvanfinityRedeemClientRequestID(input) {
				t.Fatal("normalization not deterministic")
			}
			srv, got := newAvanServer(t, func(string) (int, string) { return 200, `{"data":{"order":{"status":"completed"}}}` })
			p := NewAvanfinityV2026(avanAccount(srv.URL))
			// Retain generated identity in the request when no original was supplied.
			wireInput := input
			if input == "" {
				wireInput = effective
			}
			if _, _, err := p.Redeem(context.Background(), map[string]any{"clientRequestId": wireInput}, "device"); err != nil {
				t.Fatal(err)
			}
			if wire := (*got)[0].body["clientRequestId"]; wire != effective {
				t.Fatalf("wire=%v helper=%s", wire, effective)
			}
		})
	}
}
