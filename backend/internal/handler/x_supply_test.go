package handler

import (
	"testing"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
)

func TestXSupplyPlansConsistent(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range xSupplyPlans {
		if seen[p.Key] {
			t.Fatalf("duplicate key %s", p.Key)
		}
		seen[p.Key] = true
		if len(p.options()) == 0 {
			t.Fatalf("%s has no source", p.Key)
		}
		if !p.supports(p.Fallback) {
			t.Fatalf("%s fallback %s not supported", p.Key, p.Fallback)
		}
		if p.SpaceX != "" && !cardplatform.IsXPremiumPlan(p.SpaceX) {
			t.Fatalf("%s spacex plan %s not an X plan", p.Key, p.SpaceX)
		}
		if p.supports("off") || p.supports("") {
			t.Fatalf("%s accepts invalid source", p.Key)
		}
	}
}
