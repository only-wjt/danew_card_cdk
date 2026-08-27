package provider

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/danew/cdk-recharge-system/internal/db"
)

// Route 一次兑换请求最终打向哪个卡台。
type Route struct {
	IsSite     bool
	SiteCodeID int64
	SiteCode   string
	// RemoteCode 转发给上游的真码；legacy 时等于用户输入的码。
	RemoteCode string
	BindingID  int64
	Provider   CardProvider
	Account    db.CardPlatformAccount
	// FailoverUsed 本次是否切到了备台。
	FailoverUsed   bool
	FailoverReason string
}

// shouldFailover 上游「不可达」才切台。业务拒绝（4xx）说明码状态已定，切台会重复扣费。
func shouldFailover(status int, err error) bool {
	if err != nil {
		return true
	}
	return status == 0 || status >= 500 || status == 429
}

// ResolveLegacy 老码：按发码时的账户走，切台会打到不认识这张码的台上，所以从不 failover。
func ResolveLegacy(code string) (*Route, error) {
	code = strings.TrimSpace(code)
	acc, err := db.CardPlatformAccountForLegacyCode(code)
	if err != nil {
		return nil, err
	}
	p, err := Build(acc)
	if err != nil {
		return nil, err
	}
	return &Route{RemoteCode: code, Provider: p, Account: acc}, nil
}

// ResolveSticky 本站码在 preview 之后的三步：只认已选定的 binding。
// redemption_token 属于某一台，此时换台等于换了一次会话，必须拒绝而不是静默切。
func ResolveSticky(siteCode string) (*Route, error) {
	row, ok := db.GetSiteCDKByCode(siteCode)
	if !ok {
		return nil, fmt.Errorf("卡密不存在")
	}
	if row.CodeKind != db.CodeKindSite {
		return ResolveLegacy(siteCode)
	}
	bindings, err := db.ListBindingsForSiteCodeID(row.ID)
	if err != nil {
		return nil, err
	}
	activeID, _ := db.GetActiveBindingID(row.ID)
	var chosen *db.SiteCDKBinding
	for i := range bindings {
		if activeID > 0 && bindings[i].ID == activeID {
			chosen = &bindings[i]
			break
		}
	}
	if chosen == nil {
		// 没走过 preview（或 active 被清）时退回主绑，让流程还能继续。
		for i := range bindings {
			if bindings[i].IsPrimary && bindings[i].Status != db.BindingStatusRefunded {
				chosen = &bindings[i]
				break
			}
		}
	}
	if chosen == nil {
		return nil, fmt.Errorf("卡密未绑定可用卡台")
	}
	p, acc, err := ForAccount(chosen.AccountID)
	if err != nil {
		return nil, err
	}
	return &Route{
		IsSite: true, SiteCodeID: row.ID, SiteCode: row.Code,
		RemoteCode: chosen.RemoteCode, BindingID: chosen.ID,
		Provider: p, Account: acc,
	}, nil
}

// PreviewWithFailover 本站码的选台入口：主台不可达就换备台，选中后写 active_binding_id 粘住。
// 只在 preview 切台——此时没有任何上游会话或扣费，切换是安全的。
func PreviewWithFailover(ctx context.Context, siteCode, device string) (*Route, int, []byte, error) {
	row, ok := db.GetSiteCDKByCode(siteCode)
	if !ok {
		return nil, 0, nil, fmt.Errorf("卡密不存在")
	}
	if row.CodeKind != db.CodeKindSite {
		r, err := ResolveLegacy(siteCode)
		if err != nil {
			return nil, 0, nil, err
		}
		st, raw, err := r.Provider.Preview(ctx, r.RemoteCode, device)
		return r, st, raw, err
	}
	if row.IssueStatus != db.IssueStatusActive {
		return nil, 0, nil, fmt.Errorf("卡密不可用")
	}
	bindings, err := db.ListBindingsForSiteCodeID(row.ID)
	if err != nil {
		return nil, 0, nil, err
	}

	var lastStatus int
	var lastRaw []byte
	var lastErr error
	var reasons []string
	tried := 0
	for i := range bindings {
		b := bindings[i]
		if b.Status == db.BindingStatusRefunded || b.Status == db.BindingStatusRetired ||
			b.Status == db.BindingStatusDisabled || b.Status == db.BindingStatusConsumed {
			continue
		}
		p, acc, perr := ForAccount(b.AccountID)
		if perr != nil {
			reasons = append(reasons, fmt.Sprintf("account %d: %v", b.AccountID, perr))
			continue
		}
		tried++
		st, raw, cerr := p.Preview(ctx, b.RemoteCode, device)
		if !shouldFailover(st, cerr) {
			route := &Route{
				IsSite: true, SiteCodeID: row.ID, SiteCode: row.Code,
				RemoteCode: b.RemoteCode, BindingID: b.ID,
				Provider: p, Account: acc,
				FailoverUsed:   !b.IsPrimary,
				FailoverReason: strings.Join(reasons, "; "),
			}
			if st >= 200 && st < 300 {
				// 只有真的可兑换才粘住，避免把「码无效」的台锁成 active。
				_ = db.SetActiveBinding(row.ID, b.ID)
				_ = db.MarkCardPlatformAccountOK(acc.ID)
				if route.FailoverUsed {
					log.Printf("[failover] site=%s switched to account=%d reason=%s", row.Code, acc.ID, route.FailoverReason)
				}
			}
			return route, st, raw, nil
		}
		reason := fmt.Sprintf("%s 不可达(status=%d err=%v)", acc.Name, st, cerr)
		reasons = append(reasons, reason)
		_ = db.MarkCardPlatformAccountError(acc.ID, reason)
		lastStatus, lastRaw, lastErr = st, raw, cerr
	}
	if tried == 0 {
		return nil, 0, nil, fmt.Errorf("卡密未绑定可用卡台")
	}
	if lastErr != nil {
		return nil, lastStatus, lastRaw, fmt.Errorf("全部卡台不可达：%s", strings.Join(reasons, "; "))
	}
	return nil, lastStatus, lastRaw, nil
}
