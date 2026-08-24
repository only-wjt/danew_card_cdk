package db

import "testing"

func TestEffectiveAgentPlanPriceFallback(t *testing.T) {
	defaults := AgentPlanPriceMap{"plus": 1500, "pro_5x": 5000}
	overrides := AgentPlanPriceMap{"plus": 2250}

	if got := EffectiveAgentPlanPrice("plus", overrides, defaults); got != 2250 {
		t.Fatalf("override plus = %v, want 2250", got)
	}
	if got := EffectiveAgentPlanPrice("pro_5x", overrides, defaults); got != 5000 {
		t.Fatalf("default pro_5x = %v, want 5000", got)
	}
	if got := EffectiveAgentPlanPrice("pro_20x", overrides, defaults); got != 0 {
		t.Fatalf("missing plan = %v, want 0", got)
	}
	if got := EffectiveAgentPlanPrice("plus", nil, defaults); got != 1500 {
		t.Fatalf("nil override = %v, want 1500", got)
	}
	if got := EffectiveAgentPlanPrice("plus", AgentPlanPriceMap{}, nil); got != 0 {
		t.Fatalf("empty maps = %v, want 0", got)
	}
}

func TestAgentPlanPricePersistence(t *testing.T) {
	openTestDB(t)

	if err := SetAgentDefaultPlanPrices(AgentPlanPriceMap{"plus": 1500, "pro_5x": 5000}); err != nil {
		t.Fatalf("set defaults: %v", err)
	}
	defaults, err := GetAgentDefaultPlanPrices()
	if err != nil {
		t.Fatalf("get defaults: %v", err)
	}
	if defaults["plus"] != 1500 || defaults["pro_5x"] != 5000 {
		t.Fatalf("defaults = %#v", defaults)
	}

	a, err := CreateAgentUser("fee-a", "Str0ngPassw0rd2026", "甲", nil)
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := CreateAgentUser("fee-b", "Str0ngPassw0rd2026", "乙", nil)
	if err != nil {
		t.Fatalf("create b: %v", err)
	}

	if err := ReplaceAgentPlanPriceOverrides(a.ID, AgentPlanPriceMap{"plus": 3000}); err != nil {
		t.Fatalf("override a: %v", err)
	}

	effA, ovA, def, err := LoadAgentEffectivePlanPrices(a.ID, []string{"plus", "pro_5x", "pro_20x"})
	if err != nil {
		t.Fatalf("load a: %v", err)
	}
	if def["plus"] != 1500 {
		t.Fatalf("shared defaults leaked? %#v", def)
	}
	if ovA["plus"] != 3000 || len(ovA) != 1 {
		t.Fatalf("a overrides = %#v", ovA)
	}
	if effA["plus"] != 3000 || effA["pro_5x"] != 5000 || effA["pro_20x"] != 0 {
		t.Fatalf("a effective = %#v", effA)
	}

	effB, ovB, _, err := LoadAgentEffectivePlanPrices(b.ID, []string{"plus", "pro_5x"})
	if err != nil {
		t.Fatalf("load b: %v", err)
	}
	if len(ovB) != 0 {
		t.Fatalf("b must not see a overrides: %#v", ovB)
	}
	if effB["plus"] != 1500 || effB["pro_5x"] != 5000 {
		t.Fatalf("b effective = %#v", effB)
	}

	if err := ReplaceAgentPlanPriceOverrides(a.ID, AgentPlanPriceMap{}); err != nil {
		t.Fatalf("clear a: %v", err)
	}
	effA, ovA, _, err = LoadAgentEffectivePlanPrices(a.ID, []string{"plus"})
	if err != nil {
		t.Fatalf("reload a: %v", err)
	}
	if len(ovA) != 0 || effA["plus"] != 1500 {
		t.Fatalf("after clear: ov=%#v eff=%#v", ovA, effA)
	}
}

func TestNormalizePlanPriceMapRejectsBadInput(t *testing.T) {
	if _, err := normalizePlanPriceMap(AgentPlanPriceMap{"plus;drop": 100}); err == nil {
		t.Fatal("unsafe key should fail")
	}
	if _, err := normalizePlanPriceMap(AgentPlanPriceMap{"plus": -1}); err == nil {
		t.Fatal("negative price should fail")
	}
	if _, err := normalizePlanPriceMap(AgentPlanPriceMap{"plus": agentPlanPriceMaxCents + 1}); err == nil {
		t.Fatal("oversize price should fail")
	}
	if got, err := NormalizePlanPricesFromYuan(map[string]float64{"plus": 29.99}); err != nil {
		t.Fatalf("yuan: %v", err)
	} else if got["plus"] != 2999 {
		t.Fatalf("29.99 yuan = %v cents, want 2999", got["plus"])
	}
}

func TestYuanToPriceCents(t *testing.T) {
	if YuanToPriceCents(1.5) != 150 {
		t.Fatalf("1.5 yuan")
	}
	if YuanToPriceCents(29.99) != 2999 {
		t.Fatalf("29.99 yuan")
	}
}

func TestPlanPriceRoundTripFromYuan(t *testing.T) {
	openTestDB(t)

	prices, err := NormalizePlanPricesFromYuan(map[string]float64{
		"plus":    111,
		"pro_5x":  66,
		"pro_20x": 1100,
	})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if err := SetAgentDefaultPlanPrices(prices); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := GetAgentDefaultPlanPrices()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got["plus"] != 11100 || got["pro_5x"] != 6600 || got["pro_20x"] != 110000 {
		t.Fatalf("round trip = %#v", got)
	}
}
