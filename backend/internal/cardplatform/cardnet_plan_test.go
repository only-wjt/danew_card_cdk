package cardplatform

import "testing"

func TestCardnetCanonicalPlan(t *testing.T) {
	ok := []struct{ in, plan, product string }{
		{"plus", "plus", CardnetProductGPT},
		{" Pro_20x ", "pro_20x", CardnetProductGPT},
		{"credit500", "credit500", CardnetProductGPT},
		{"grok_premium_monthly", "grok_premium_monthly", CardnetProductGrok},
		{"GROK_Ultra_Yearly", "grok_ultra_yearly", CardnetProductGrok},
		// 旧键换新键
		{"grok_lite_monthly", "grok_plus_monthly", CardnetProductGrok},
		{"grok_monthly", "grok_premium_monthly", CardnetProductGrok},
		{"grok_heavy_yearly", "grok_ultra_yearly", CardnetProductGrok},
	}
	for _, tc := range ok {
		plan, product, err := CardnetCanonicalPlan(tc.in)
		if err != nil || plan != tc.plan || product != tc.product {
			t.Errorf("%q: got %q %q %v", tc.in, plan, product, err)
		}
	}
	bad := []string{"", "premium_monthly", "x_basic_monthly", "premium_plus_yearly", "plus_monthly", "monthly", "grok_foo"}
	for _, in := range bad {
		if plan, _, err := CardnetCanonicalPlan(in); err == nil {
			t.Errorf("%q should be rejected, got %q", in, plan)
		}
	}
}
