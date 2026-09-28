package handler

import "testing"

func TestBatchPlanAllowedFollowsSellableRegistry(t *testing.T) {
	sellable := map[string]bool{
		"plus":          true,
		"pro_5x":        true,
		"pro_20x":       true,
		"pro_20x_renew": true,
	}
	if !batchPlanAllowed("pro_20x_renew", sellable) {
		t.Fatal("live registry must allow Pro 20x renew")
	}
	if batchPlanAllowed("claude_pro", sellable) {
		t.Fatal("non-sellable plan must stay blocked")
	}
	if batchPlanAllowed("", sellable) {
		t.Fatal("empty plan")
	}
}

func TestBatchPlanAllowedFallbackWithoutRegistry(t *testing.T) {
	if !batchPlanAllowed("plus", nil) || !batchPlanAllowed("pro_20x", nil) {
		t.Fatal("fallback should keep the three subscription plans")
	}
	if batchPlanAllowed("pro_20x", map[string]bool{}) {
		t.Fatal("empty live catalog must not fall back to guessed plans")
	}
	if batchPlanAllowed("pro_20x_renew", nil) {
		t.Fatal("renew must not be guessed when the card platform is offline")
	}
	if batchPlanAllowed("credit2500", map[string]bool{"credit2500": true, "plus": true}) {
		t.Fatal("credit plans are not batch subscription recharges")
	}
}

func TestBatchPlanAvailableSorted(t *testing.T) {
	got := batchPlanAvailable(map[string]bool{"pro_20x_renew": true, "plus": true})
	if len(got) != 2 || got[0] != "plus" || got[1] != "pro_20x_renew" {
		t.Fatalf("got %#v", got)
	}
	if got := batchPlanAvailable(nil); len(got) != 3 {
		t.Fatalf("fallback %#v", got)
	}
}
