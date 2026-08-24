package handler

import (
	"testing"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
)

func TestMergePricedPlansKeepsCoreWithoutLive(t *testing.T) {
	got := mergePricedPlans(corePricedPlans(), nil)
	want := []string{"plus", "pro_5x", "pro_20x", "pro", "go"}
	seen := map[string]string{}
	for _, p := range got {
		seen[p.Key] = p.Label
	}
	for _, key := range want {
		if _, ok := seen[key]; !ok {
			t.Fatalf("missing core plan %s in %#v", key, seen)
		}
	}
	if seen["pro_20x"] != "Pro 20x" {
		t.Fatalf("pro_20x label = %q, want Pro 20x", seen["pro_20x"])
	}
}

func TestMergePricedPlansAddsLiveExtrasAndLabels(t *testing.T) {
	live := []cardplatform.SellablePlan{
		{Key: "plus", Label: "ChatGPT Plus"},
		{Key: "ultra", Label: "Ultra"},
	}
	got := mergePricedPlans(corePricedPlans(), live)
	var plus, ultra pricedPlanMeta
	for _, p := range got {
		if p.Key == "plus" {
			plus = p
		}
		if p.Key == "ultra" {
			ultra = p
		}
	}
	if plus.Label != "ChatGPT Plus" {
		t.Fatalf("live label not merged: %#v", plus)
	}
	if ultra.Key != "ultra" || ultra.Label != "Ultra" {
		t.Fatalf("live extra missing: %#v", ultra)
	}
}

func TestCoreSellableFallbackHasUserNamedPlans(t *testing.T) {
	got := coreSellableFallbackPlans()
	if len(got) == 0 || got[0].Key != "plus" || got[1].Key != "pro_5x" || got[2].Key != "pro_20x" {
		t.Fatalf("fallback order = %#v", got)
	}
}
