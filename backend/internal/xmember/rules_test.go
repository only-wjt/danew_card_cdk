package xmember

import (
	"testing"

	"github.com/danew/cdk-recharge-system/internal/db"
)

func TestLimitsReadyRejectsFundingAboveWallet(t *testing.T) {
	lim := db.XPlanLimit{Plan: "premium_12m", Enabled: true, Currency: "usd", MaxOfficialAmountMinor: 9000, MaxWalletDebitUSD: "1", FundingAmountUSD: "222"}
	if err := limitsReady(db.XChannelCDK, lim); err == nil {
		t.Fatal("wallet 1 < funding 222 should be rejected")
	}
	lim.MaxWalletDebitUSD = "260"
	if err := limitsReady(db.XChannelCDK, lim); err != nil {
		t.Fatalf("unexpected %v", err)
	}
}

func TestNormalizeHandle(t *testing.T) {
	cases := map[string]string{
		"@Example_User":                 "Example_User",
		"https://x.com/example_user":    "example_user",
		"https://twitter.com/Ab_1?s=1":  "Ab_1",
		"  x.com/name/extra ":           "name",
	}
	for in, want := range cases {
		if got := NormalizeHandle(in); got != want {
			t.Fatalf("%q -> %q, want %q", in, got, want)
		}
	}
	if ValidHandle("bad name") || !ValidHandle("@ok_name") {
		t.Fatal("handle validation")
	}
}

func TestUSDToE4(t *testing.T) {
	v, ok := USDToE4("15.40")
	if !ok || v != 154000 {
		t.Fatalf("%d %v", v, ok)
	}
	if E4ToUSD(154000) != "15.4000" {
		t.Fatal(E4ToUSD(154000))
	}
}

func TestReleaseOnlyWhenMoneyUntouched(t *testing.T) {
	yes := true
	no := false
	if !ShouldRelease("ineligible", false, false, nil) {
		t.Fatal("ineligible without money should release")
	}
	if ShouldRelease("ineligible", true, false, nil) {
		t.Fatal("payment attempted must lock")
	}
	if ShouldRelease("failed_precharge", false, false, &no) {
		t.Fatal("canRetry false must lock")
	}
	if !ShouldRelease("paying", false, false, &yes) {
		t.Fatal("explicit canRetry must release")
	}
	if ShouldRelease("cancelled", false, true, nil) {
		t.Fatal("funding dispatched must lock")
	}
}
