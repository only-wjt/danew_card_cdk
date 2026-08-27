package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/danew/cdk-recharge-system/internal/db"
)

// fakeProvider 可编排的假卡台，用来验证双发回滚与切台判定。
type fakeProvider struct {
	accountID   int64
	issueErr    error
	previewCode int
	previewErr  error

	mu       sync.Mutex
	issued   []string
	prefs    []IssuePreference
	deleted  []string
	disabled []string
	// refundUnsupported 模拟上游没开放删除退款接口。
	refundUnsupported bool
	deleteErr         error
}

func (f *fakeProvider) Protocol() string { return "fake" }
func (f *fakeProvider) AccountID() int64 { return f.accountID }

func (f *fakeProvider) IssueCDK(ctx context.Context, plan, idem string, pref IssuePreference) (*IssuedUpstream, error) {
	if f.issueErr != nil {
		return nil, f.issueErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prefs = append(f.prefs, pref)
	id := fmt.Sprintf("a%d-%d", f.accountID, len(f.issued)+1)
	f.issued = append(f.issued, id)
	return &IssuedUpstream{
		RemoteID:   id,
		RemoteCode: fmt.Sprintf("UP-%s", id),
		CodePrefix: "UP-",
		Plan:       plan,
		FeeMinor:   100,
	}, nil
}

func (f *fakeProvider) Preview(ctx context.Context, remoteCode, device string) (int, []byte, error) {
	if f.previewErr != nil {
		return 0, nil, f.previewErr
	}
	st := f.previewCode
	if st == 0 {
		st = 200
	}
	return st, []byte(fmt.Sprintf(`{"redemption_token":"tok-%d","code":%q}`, f.accountID, remoteCode)), nil
}

func (f *fakeProvider) Preflight(ctx context.Context, body map[string]any, device string) (int, []byte, error) {
	return 200, []byte(`{}`), nil
}

func (f *fakeProvider) Redeem(ctx context.Context, body map[string]any, device string) (int, []byte, error) {
	return 200, []byte(`{}`), nil
}

func (f *fakeProvider) Result(ctx context.Context, token, device string) (int, []byte, error) {
	return 200, []byte(`{"status":"completed"}`), nil
}

func (f *fakeProvider) Disable(ctx context.Context, remoteID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disabled = append(f.disabled, remoteID)
	return nil
}

func (f *fakeProvider) DeleteAndRefund(ctx context.Context, remoteID string) error {
	if f.refundUnsupported {
		return ErrRefundUnsupported
	}
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, remoteID)
	return nil
}

func TestShouldFailoverOnlyForUnreachable(t *testing.T) {
	cases := []struct {
		name   string
		status int
		err    error
		want   bool
	}{
		{"transport error", 0, errors.New("dial tcp: timeout"), true},
		{"no response", 0, nil, true},
		{"upstream 500", 500, nil, true},
		{"upstream 502", 502, nil, true},
		{"rate limited", 429, nil, true},
		// 4xx 说明上游认得这张码并明确拒绝，切台只会在另一台重复扣费。
		{"code invalid", 400, nil, false},
		{"already used", 409, nil, false},
		{"not found", 404, nil, false},
		{"ok", 200, nil, false},
	}
	for _, tc := range cases {
		if got := shouldFailover(tc.status, tc.err); got != tc.want {
			t.Errorf("%s: shouldFailover(%d, %v) = %v, want %v", tc.name, tc.status, tc.err, got, tc.want)
		}
	}
}

func TestDualEligiblePlan(t *testing.T) {
	for _, p := range []string{"gpt_plus_1m", "gpt_pro_1m", ""} {
		if !dualEligiblePlan(p) {
			t.Errorf("plan %q should be dual eligible", p)
		}
	}
	for _, p := range []string{"credit250", "CREDIT500", " credit1000 "} {
		if dualEligiblePlan(p) {
			t.Errorf("plan %q must not be dual eligible", p)
		}
	}
}

// TestReclaimUpstreamFallsBackToDisable 删除退款不被支持时必须降级 disable，
// 否则回滚出来的上游码还留在可兑换状态。
func TestReclaimUpstreamFallsBackToDisable(t *testing.T) {
	f := &fakeProvider{accountID: 1, refundUnsupported: true}
	reclaimUpstream(context.Background(), f, "a1-1")
	if len(f.disabled) != 1 || f.disabled[0] != "a1-1" {
		t.Fatalf("expected disable fallback, got disabled=%v deleted=%v", f.disabled, f.deleted)
	}
	if len(f.deleted) != 0 {
		t.Fatalf("refund unsupported but delete recorded: %v", f.deleted)
	}
}

func TestReclaimUpstreamPrefersRefund(t *testing.T) {
	f := &fakeProvider{accountID: 1}
	reclaimUpstream(context.Background(), f, "a1-1")
	if len(f.deleted) != 1 {
		t.Fatalf("expected delete+refund, got %v", f.deleted)
	}
	if len(f.disabled) != 0 {
		t.Fatalf("refund succeeded but also disabled: %v", f.disabled)
	}
}

func TestNewSiteCodeIsUniqueAndPrefixed(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		code, err := NewSiteCode()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(code, SiteCodePrefix) {
			t.Fatalf("code %q missing prefix", code)
		}
		if !IsSiteCode(code) {
			t.Fatalf("IsSiteCode(%q) = false", code)
		}
		if seen[code] {
			t.Fatalf("duplicate site code %q", code)
		}
		seen[code] = true
	}
	for _, legacy := range []string{"GPTD-1111-2222", "SXC-AAAA-BBBB", "AVF-XXXX", ""} {
		if IsSiteCode(legacy) {
			t.Fatalf("legacy code %q must not be treated as site code", legacy)
		}
	}
}

// TestDualIssueRollbackKeepsNoUsableCode 一台成功一台失败时，本站码必须作废，
// 已买到的那张要收回；否则会卖出一张只能单台兑换、且账已扣的码。
func TestDualIssueRollbackKeepsNoUsableCode(t *testing.T) {
	withTestDB(t)
	seedAccounts(t, 2)

	good := &fakeProvider{accountID: 1}
	bad := &fakeProvider{accountID: 2, issueErr: errors.New("upstream 503")}
	restore := stubBuild(map[int64]CardProvider{1: good, 2: bad})
	defer restore()

	if _, err := DualIssueOne(context.Background(), "gpt_plus_1m", false); err == nil {
		t.Fatal("expected dual issue to fail when one platform is down")
	}
	if len(good.deleted) != 1 {
		t.Fatalf("issued upstream code not reclaimed: deleted=%v disabled=%v", good.deleted, good.disabled)
	}

	var active int
	if err := db.DB.QueryRow(`
		SELECT COUNT(*) FROM cardplatform_cdk_codes
		WHERE code_kind = 'site' AND issue_status = 'active'
	`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("rollback left %d active site code(s)", active)
	}
}

func TestDualIssueSuccessBindsBothPlatforms(t *testing.T) {
	withTestDB(t)
	seedAccounts(t, 2)
	if err := db.SetCardSelectionRulesForAccount(1, []db.CardSelectionRule{
		{PlanKey: "A-PRODUCT", Channel: "one", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetCardSelectionRulesForAccount(2, []db.CardSelectionRule{
		{PlanKey: "B-PRODUCT", Channel: "two", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}

	a := &fakeProvider{accountID: 1}
	b := &fakeProvider{accountID: 2}
	restore := stubBuild(map[int64]CardProvider{1: a, 2: b})
	defer restore()

	res, err := DualIssueOne(context.Background(), "gpt_plus_1m", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Bindings != 2 || !res.DualEligible {
		t.Fatalf("expected 2 bindings dual-eligible, got %+v", res)
	}
	if res.TotalFee != 200 {
		t.Fatalf("expected fee sum of both platforms, got %d", res.TotalFee)
	}
	if len(a.prefs) != 1 || a.prefs[0].SegmentKey != "A-PRODUCT" {
		t.Fatalf("A preference not applied: %+v", a.prefs)
	}
	if len(b.prefs) != 1 || b.prefs[0].SegmentKey != "B-PRODUCT" {
		t.Fatalf("B preference not applied: %+v", b.prefs)
	}
	bindings, err := db.ListBindingsForSiteCode(res.SiteCode)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 2 {
		t.Fatalf("expected 2 bindings in db, got %d", len(bindings))
	}
	if !bindings[0].IsPrimary {
		t.Fatal("primary binding must sort first")
	}
	for _, bd := range bindings {
		if bd.RemoteCode == res.SiteCode {
			t.Fatal("remote code must differ from site code")
		}
	}
	row, ok := db.GetSiteCDKByCode(res.SiteCode)
	if !ok || row.IssueStatus != db.IssueStatusActive {
		t.Fatalf("site code not activated: %+v ok=%v", row, ok)
	}
}

func TestDualIssueRefusesSilentSingleBind(t *testing.T) {
	withTestDB(t)
	seedAccounts(t, 1)
	fake := &fakeProvider{accountID: 1}
	restore := stubBuild(map[int64]CardProvider{1: fake})
	defer restore()
	if _, err := DualIssueOne(context.Background(), "plus", false); err == nil {
		t.Fatal("expected dual issue to reject fewer than two usable accounts")
	}
}

// TestRetireSiblingsAfterConsume 核销后另一台那张必须收回，
// 否则同一本站码能在两个台各兑一次。
func TestRetireSiblingsAfterConsume(t *testing.T) {
	withTestDB(t)
	seedAccounts(t, 2)

	a := &fakeProvider{accountID: 1}
	b := &fakeProvider{accountID: 2}
	restore := stubBuild(map[int64]CardProvider{1: a, 2: b})
	defer restore()

	res, err := DualIssueOne(context.Background(), "gpt_plus_1m", false)
	if err != nil {
		t.Fatal(err)
	}
	bindings, _ := db.ListBindingsForSiteCode(res.SiteCode)
	consumed := bindings[0]

	RetireSiblings(context.Background(), res.SiteCodeID, consumed.ID)

	if len(b.deleted) != 1 {
		t.Fatalf("sibling upstream code not refunded: deleted=%v disabled=%v", b.deleted, b.disabled)
	}
	after, _ := db.ListBindingsForSiteCodeID(res.SiteCodeID)
	for _, bd := range after {
		if bd.ID == consumed.ID {
			continue
		}
		if bd.Status != db.BindingStatusRefunded {
			t.Fatalf("sibling binding status = %q, want refunded", bd.Status)
		}
	}
}
