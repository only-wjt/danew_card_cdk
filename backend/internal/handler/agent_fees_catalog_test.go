package handler

import (
	"testing"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
)

func TestMergePricedPlansKeepsCoreWithoutLive(t *testing.T) {
	got := mergePricedPlans(corePricedPlans(), nil)
	want := []string{"plus", "pro_5x", "pro_20x", "pro", "go", "gpt_white"}
	seen := map[string]string{}
	order := make([]string, 0, len(got))
	for _, p := range got {
		seen[p.Key] = p.Label
		order = append(order, p.Key)
	}
	for _, key := range want {
		if _, ok := seen[key]; !ok {
			t.Fatalf("missing core plan %s in %#v", key, seen)
		}
	}
	if seen["pro_20x"] != "Pro 20x" {
		t.Fatalf("pro_20x label = %q, want Pro 20x", seen["pro_20x"])
	}
	if seen["gpt_white"] != "GPT白号" {
		t.Fatalf("gpt_white label = %q", seen["gpt_white"])
	}
	goIdx, whiteIdx := -1, -1
	for i, key := range order {
		if key == "go" {
			goIdx = i
		}
		if key == "gpt_white" {
			whiteIdx = i
		}
	}
	if goIdx < 0 || whiteIdx < 0 || whiteIdx <= goIdx {
		t.Fatalf("gpt_white should follow go: %v", order)
	}
}

func TestMergePricedPlansAddsLiveExtrasAndLabels(t *testing.T) {
	live := []cardplatform.SellablePlan{
		{Key: "plus", Label: "ChatGPT Plus"},
		{Key: "ultra", Label: "Ultra"},
	}
	got := mergePricedPlans(corePricedPlans(), live)
	var plus, ultra, white pricedPlanMeta
	for _, p := range got {
		if p.Key == "plus" {
			plus = p
		}
		if p.Key == "ultra" {
			ultra = p
		}
		if p.Key == "gpt_white" {
			white = p
		}
	}
	if plus.Label != "ChatGPT Plus" {
		t.Fatalf("live label not merged: %#v", plus)
	}
	if ultra.Key != "ultra" || ultra.Label != "Ultra" {
		t.Fatalf("live extra missing: %#v", ultra)
	}
	if white.Key != "gpt_white" {
		t.Fatalf("local stock missing when live catalog exists: %#v", got)
	}
}

func TestResolveAgentPlanCatalogKeepsLocalStockWithLive(t *testing.T) {
	live := []cardplatform.SellablePlan{{Key: "plus", Label: "ChatGPT Plus"}}
	got := mergePricedPlans(localStockPlans(), live)
	var white, plus pricedPlanMeta
	for _, p := range got {
		if p.Key == "gpt_white" {
			white = p
		}
		if p.Key == "plus" {
			plus = p
		}
	}
	if white.Key != "gpt_white" || plus.Key != "plus" {
		t.Fatalf("merge local+live = %#v", got)
	}
}

func TestCoreSellableFallbackHasUserNamedPlans(t *testing.T) {
	got := coreSellableFallbackPlans()
	if len(got) == 0 || got[0].Key != "plus" || got[1].Key != "pro_5x" || got[2].Key != "pro_20x" {
		t.Fatalf("fallback order = %#v", got)
	}
}
