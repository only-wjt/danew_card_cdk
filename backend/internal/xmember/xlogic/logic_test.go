package xlogic

import (
	"testing"
	"time"
)

func TestPollNeverConfirmsAQuote(t *testing.T) {
	// 旧 bug：CDK 报价成功后带有 message，轮询把它当成可以付款。
	cases := []struct {
		code, up, order, want string
	}{
		{"quoted", "prepared", "", ActionIdle},
		{"quoted", "", "", ActionIdle},
		{"quoted", "preparing", "", ActionIdle},
		{"quoted", "queued_confirm", "", ActionConfirm},
		{"quoted", "queued_quote", "", ActionQuote},
		{"unused", "queued_quote", "", ActionQuote},
		{"unused", "", "", ActionQuote},
		{"funded", "funded", "", ActionRefresh},
		{"paying", "paying", "ord-1", ActionRefresh},
		{"quoted", "prepared", "ord-1", ActionIdle},
	}
	for _, c := range cases {
		if got := PollAction(c.code, c.up, c.order); got != c.want {
			t.Fatalf("PollAction(%s,%s,%s)=%s want %s", c.code, c.up, c.order, got, c.want)
		}
	}
}

func TestReleaseUsesLocalMoneyFlags(t *testing.T) {
	yes := true
	no := false
	if !ShouldRelease("ineligible", false, false, nil) {
		t.Fatal("ineligible before any redeem should release")
	}
	if ShouldRelease("ineligible", true, false, nil) {
		t.Fatal("ineligible after redeem must lock")
	}
	if ShouldRelease("ineligible", false, true, nil) {
		t.Fatal("ineligible after funding must lock")
	}
	if !ShouldRelease("ineligible", true, true, &yes) {
		t.Fatal("canRetry true is an explicit upstream release")
	}
	if ShouldRelease("failed_precharge", false, false, &no) {
		t.Fatal("canRetry false must lock")
	}
	if ShouldRelease("failed_precharge", false, false, nil) {
		t.Fatal("failed_precharge without canRetry must lock")
	}
	if !ShouldRelease("cancelled", false, false, nil) {
		t.Fatal("cancelled direct quote with no payment should release")
	}
	if ShouldRelease("cancelled", true, false, nil) {
		t.Fatal("cancelled after payment must lock")
	}
	if ShouldRelease("prepared", false, false, nil) {
		t.Fatal("prepared is not a release")
	}
}

func TestDecideMapsEveryUpstreamStatus(t *testing.T) {
	yes := true
	table := []struct {
		up, want  string
		pay, fund bool
		retry     *bool
		release   bool
	}{
		{"preparing", "quoted", false, false, nil, false},
		{"preparing", "funding", true, true, nil, false},
		{"prepared", "quoted", false, false, nil, false},
		{"funding", "funding", true, true, nil, false},
		{"funded", "funded", true, true, nil, false},
		{"processing", "paying", true, false, nil, false},
		{"paying", "paying", true, true, nil, false},
		{"completed", "completed", true, true, nil, false},
		{"paid_pending_delivery", "paid_pending_delivery", true, true, nil, false},
		{"review_required", "review_required", true, true, nil, false},
		{"requires_action", "requires_action", true, true, nil, false},
		{"revoked", "disabled", false, false, nil, false},
		{"ineligible", "unused", false, false, nil, true},
		{"ineligible", "uncertain", true, true, nil, false},
		{"ineligible", "unused", true, true, &yes, true},
		{"failed_precharge", "uncertain", true, true, nil, false},
		{"cancelled", "unused", false, false, nil, true},
		{"cancelled", "uncertain", true, false, nil, false},
		{"unused", "unused", false, false, nil, false},
		{"something_new", "uncertain", false, false, nil, false},
	}
	for _, row := range table {
		d := DecideStatus("quoted", row.up, row.pay, row.fund, row.retry)
		if d.CodeStatus != row.want || d.Release != row.release {
			t.Fatalf("%s pay=%v fund=%v → %+v want %s release=%v", row.up, row.pay, row.fund, d, row.want, row.release)
		}
	}
}

func TestSecondRedeemOnlyOnce(t *testing.T) {
	if !ShouldSecondRedeem("tg_cdk", "funded", false) {
		t.Fatal("tg cdk second redeem")
	}
	if !ShouldSecondRedeem("x_cdk", "funded", false) {
		t.Fatal("funded CDK still needs the payment redeem")
	}
	if ShouldSecondRedeem("x_cdk", "funded", true) {
		t.Fatal("payment already sent")
	}
	if ShouldSecondRedeem("x_cdk", "quoted", false) {
		t.Fatal("quote must not redeem")
	}
	if ShouldSecondRedeem("x_direct", "funded", false) {
		t.Fatal("direct has no second redeem")
	}
}

func TestNoteFundingIgnoresMissingPublicFlags(t *testing.T) {
	fund, pay := NoteFunding("prepared", nil)
	if fund || pay {
		t.Fatal("prepared quote did not move money")
	}
	fund, pay = NoteFunding("funded", nil)
	if !fund || !pay {
		t.Fatal("funded means the first redeem moved money")
	}
	yes := true
	fund, pay = NoteFunding("ineligible", &yes)
	if fund || pay {
		t.Fatal("explicit canRetry means no money")
	}
	fund, pay = NoteFunding("ineligible", nil)
	if !fund || !pay {
		t.Fatal("ineligible after redeem without canRetry stays locked")
	}
}

func TestRecipientSwitch(t *testing.T) {
	if RecipientAction("alice", "alice", false) != ActReuse {
		t.Fatal("same recipient")
	}
	if RecipientAction("@alice", "alice", false) != ActReuse {
		t.Fatal("at-sign")
	}
	if RecipientAction("alice", "bob", false) != ActReplace {
		t.Fatal("new recipient before money")
	}
	if RecipientAction("alice", "bob", true) != ActReject {
		t.Fatal("cannot retarget after money")
	}
	if RecipientAction("", "bob", false) != ActReuse {
		t.Fatal("first fill")
	}
}

func TestClassifyQuoteVsPay(t *testing.T) {
	if ClassifyFailure("quote", true, 0, "") != FailRetry {
		t.Fatal("quote network should retry")
	}
	if ClassifyFailure("read", false, 503, "") != FailRetry {
		t.Fatal("result query 503 should retry")
	}
	if ClassifyFailure("pay", true, 0, "") != FailUncertain {
		t.Fatal("pay network must lock")
	}
	if ClassifyFailure("pay", false, 500, "") != FailUncertain {
		t.Fatal("pay 500 must lock")
	}
	if ClassifyFailure("quote", false, 429, "") != FailWait {
		t.Fatal("429 waits")
	}
	if ClassifyFailure("pay", false, 409, "QUOTE_EXPIRED") != FailStale {
		t.Fatal("expired quote")
	}
	if ClassifyFailure("pay", false, 409, "QUOTE_CHANGED") != FailStale {
		t.Fatal("changed quote")
	}
	if ClassifyFailure("quote", false, 422, "SPENDABLE_BALANCE_INSUFFICIENT") != FailBalance {
		t.Fatal("balance")
	}
	if ClassifyFailure("pay", false, 400, "BAD_RECIPIENT") != FailReturn {
		t.Fatal("client error")
	}
}

func TestRateLimitWindow(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	var hits []time.Time
	for i := 0; i < 5; i++ {
		ok, next := AllowRate(hits, now, 5, time.Minute)
		if !ok {
			t.Fatalf("call %d should pass", i+1)
		}
		hits = next
	}
	if ok, _ := AllowRate(hits, now, 5, time.Minute); ok {
		t.Fatal("6th prepare in the same minute must wait")
	}
	if ok, _ := AllowRate(hits, now.Add(time.Minute+time.Second), 5, time.Minute); !ok {
		t.Fatal("window should reset")
	}
}

func TestScriptedCDKDoesNotPayOnQuote(t *testing.T) {
	// 客户只拿到报价。轮询一步都不能变成 confirm，第二次 redeem 也不能提前。
	code, up, order := "quoted", "prepared", ""
	if PollAction(code, up, order) != ActionIdle {
		t.Fatal("poll paid a quote")
	}
	if ShouldSecondRedeem("x_cdk", code, false) {
		t.Fatal("second redeem before funding")
	}
	// 客户点了确认，限流排队。
	if PollAction("quoted", "queued_confirm", "") != ActionConfirm {
		t.Fatal("queued confirm should pay")
	}
	// 第一次 redeem 之后状态是 funded，且本站还没记下第二次。
	code = "funded"
	if !ShouldSecondRedeem("x_cdk", code, false) {
		t.Fatal("need second redeem")
	}
	if ShouldSecondRedeem("x_cdk", code, true) {
		t.Fatal("second redeem repeated")
	}
}
