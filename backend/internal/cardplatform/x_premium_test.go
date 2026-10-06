package cardplatform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestXPremiumCatalogueIsProductScoped(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openapi/v1/gpt-direct/plans" || r.URL.Query().Get("product") != "x" {
			t.Errorf("wrong product request %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":0,"data":{"version":1,"payment_regions":[{"country":"JP","currency":"JPY"},{"country":"NG","currency":"NGN"},{"country":"TR","currency":"TRY"},{"country":"CL","currency":"CLP"}],"plans":{"x_basic_monthly":{"enabled":true,"currency":"JPY","serviceFeeUsdMinor":15}},"registry":[{"key":"basic_monthly","label":"X Basic","flow":"direct"}]}}`))
	}))
	defer s.Close()
	c := New(Config{SiteBase: s.URL, APIKey: "fixture-key"})
	plans, err := c.GetPlans(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	rows := plans.SellablePlans()
	if len(rows) != 1 || rows[0].Key != "basic_monthly" || rows[0].ServiceFeeUsdMinor != 15 {
		t.Fatalf("bad X catalogue %+v", rows)
	}
	if len(plans.PaymentRegions) != 3 || plans.PaymentRegions[1].Country != "NG" || plans.PaymentRegions[2].Country != "TR" {
		t.Fatal("wrong X region")
	}
}
func TestXCredentialsAreNotInvoiceSessions(t *testing.T) {
	if !IsXPremiumCredential(`{"auth_token":"fixture","ct0":"fixture","billing_email":"fixture@example.test"}`) {
		t.Fatal("X credential stored as GPT session")
	}
	if IsXPremiumCredential(`{"sessionToken":"fixture"}`) {
		t.Fatal("GPT credential misclassified")
	}
	for _, plan := range []string{"basic_monthly", "premium_yearly", "premium_plus_yearly", "x_premium_monthly"} {
		if !IsXPremiumPlan(plan) {
			t.Fatal(plan)
		}
	}
	if IsXPremiumPlan("plus") || IsXPremiumPlan("grok_plus_monthly") {
		t.Fatal("cross product plan")
	}
}

func TestXPaymentCountries(t *testing.T) {
	for _, cc := range []string{"US", "JP", "PH", "NG", "TR", "EG"} {
		if c, err := XPaymentCountry(cc); err != nil || c != cc {
			t.Fatal(cc, err)
		}
	}
	if c, err := XPaymentCountry(""); err != nil || c != "JP" {
		t.Fatal("default changed")
	}
	if _, err := XPaymentCountry("CL"); err == nil {
		t.Fatal("X Chile allowed")
	}
}

func TestXCatalogueCannotFallBackToGPTOnOldServers(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":0,"data":{"plans":{"plus":{"enabled":true,"currency":"PHP"}},"registry":[{"key":"plus","label":"ChatGPT Plus"}]}}`))
	}))
	defer s.Close()
	p, err := New(Config{SiteBase: s.URL, APIKey: "fixture-key"}).GetPlans(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.SellablePlans()) != 0 {
		t.Fatal("X page offered GPT codes")
	}
}
