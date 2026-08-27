package provider

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/danew/cdk-recharge-system/internal/db"
)

// DualIssueResult 一张本站码的双发结果。
type DualIssueResult struct {
	SiteCode     string
	SiteCodeID   int64
	Bindings     int
	DualEligible bool
	TotalFee     int64
}

// dualEligiblePlan credit* 不能进新台 generate，只单绑旧台。
func dualEligiblePlan(plan string) bool {
	return !strings.HasPrefix(strings.ToLower(strings.TrimSpace(plan)), "credit")
}

// DualIssueOne 发一张本站码：先写 pending 行，再向各账户各买一张上游码。
// 主路径要求全部成功；任一失败则整张作废并尽量回收已买到的上游码。
// allowDegradedSingleBind=true 时允许「只成功一条」降级出货（默认应关）。
func DualIssueOne(ctx context.Context, plan string, allowDegradedSingleBind bool) (*DualIssueResult, error) {
	plan = strings.TrimSpace(plan)
	if plan == "" {
		return nil, fmt.Errorf("plan required")
	}
	reg, skipped, err := LoadRegistry()
	if err != nil {
		return nil, err
	}
	if len(reg.Providers) == 0 {
		if len(skipped) > 0 {
			return nil, fmt.Errorf("没有可用卡台账户：%s", strings.Join(skipped, "; "))
		}
		return nil, fmt.Errorf("未配置卡台账户，无法发码")
	}

	targets := reg.Providers
	accounts := reg.Accounts
	dual := dualEligiblePlan(plan)
	if dual && len(targets) < 2 && !allowDegradedSingleBind {
		reason := ""
		if len(skipped) > 0 {
			reason = "：" + strings.Join(skipped, "; ")
		}
		return nil, fmt.Errorf("双绑发码至少需要 2 个可用卡台，当前 %d 个%s", len(targets), reason)
	}
	if !dual {
		// credit* 只打明确标记的主台，priority 仅控制双绑顺序，不能代替主台归属。
		for i := range accounts {
			if accounts[i].IsPrimaryDefault {
				targets = []CardProvider{targets[i]}
				accounts = []db.CardPlatformAccount{accounts[i]}
				break
			}
		}
		if len(targets) > 1 {
			targets = targets[:1]
			accounts = accounts[:1]
		}
	}

	code, err := NewSiteCode()
	if err != nil {
		return nil, err
	}
	row, err := db.CreatePendingSiteCDK(code, plan, dual && len(targets) > 1, 0)
	if err != nil {
		return nil, err
	}

	type issued struct {
		accountID int64
		provider  CardProvider
		up        *IssuedUpstream
	}
	var ok []issued
	var firstErr error
	for i, p := range targets {
		acc := accounts[i]
		idem := fmt.Sprintf("dual-%s-a%d", row.Code, acc.ID)
		issuer, segmentType, segmentKey := db.PreferredCardSelectionForAccount(acc.ID)
		up, ierr := p.IssueCDK(ctx, plan, idem, IssuePreference{
			Issuer: issuer, SegmentType: segmentType, SegmentKey: segmentKey,
		})
		if ierr != nil {
			firstErr = fmt.Errorf("%s 发码失败: %w", acc.Name, ierr)
			log.Printf("[dual-issue] site=%s account=%d issue failed: %v", row.Code, acc.ID, ierr)
			break
		}
		bindingID, berr := db.InsertSiteCDKBinding(db.SiteCDKBinding{
			SiteCodeID:       row.ID,
			SiteCode:         row.Code,
			AccountID:        acc.ID,
			Provider:         p.Protocol(),
			RemoteID:         up.RemoteID,
			RemoteCode:       up.RemoteCode,
			RemoteCodePrefix: up.CodePrefix,
			IsPrimary:        i == 0,
			Status:           db.BindingStatusUnused,
			IssuedIdemKey:    idem,
		})
		if berr != nil {
			firstErr = fmt.Errorf("%s 绑定落库失败: %w", acc.Name, berr)
			// 码已经买到但没落库：立刻尽量回收，别留成上游孤儿。
			reclaimUpstream(ctx, p, up.RemoteID)
			break
		}
		_ = bindingID
		ok = append(ok, issued{accountID: acc.ID, provider: p, up: up})
	}

	wantAll := len(targets)
	if len(ok) == wantAll {
		var fee int64
		for _, it := range ok {
			fee += it.up.FeeMinor
		}
		if err := db.ActivateSiteCDK(row.ID, fee); err != nil {
			return nil, err
		}
		return &DualIssueResult{
			SiteCode: row.Code, SiteCodeID: row.ID, Bindings: len(ok),
			DualEligible: dual && wantAll > 1, TotalFee: fee,
		}, nil
	}

	if allowDegradedSingleBind && len(ok) >= 1 {
		var fee int64
		for _, it := range ok {
			fee += it.up.FeeMinor
		}
		if err := db.ActivateSiteCDK(row.ID, fee); err != nil {
			return nil, err
		}
		log.Printf("[dual-issue] DEGRADED single-bind site=%s bindings=%d reason=%v", row.Code, len(ok), firstErr)
		return &DualIssueResult{
			SiteCode: row.Code, SiteCodeID: row.ID, Bindings: len(ok),
			DualEligible: false, TotalFee: fee,
		}, nil
	}

	// 主路径：整张失败。已买到的上游码尽量删除退款，拿不回来至少 disable。
	for _, it := range ok {
		reclaimUpstream(ctx, it.provider, it.up.RemoteID)
		_ = db.MarkBindingReclaimed(row.ID, it.accountID)
	}
	_ = db.FailSiteCDK(row.ID)
	if firstErr == nil {
		firstErr = fmt.Errorf("双发未完成")
	}
	return nil, firstErr
}

// reclaimUpstream 回收未使用的上游码：删除退款优先，不支持则 disable。
func reclaimUpstream(ctx context.Context, p CardProvider, remoteID string) {
	if p == nil || strings.TrimSpace(remoteID) == "" {
		return
	}
	if err := p.DeleteAndRefund(ctx, remoteID); err == nil {
		return
	} else if err != ErrRefundUnsupported {
		log.Printf("[dual-issue] delete+refund failed remote=%s: %v", remoteID, err)
	}
	if err := p.Disable(ctx, remoteID); err != nil {
		log.Printf("[dual-issue] disable fallback failed remote=%s: %v", remoteID, err)
	}
}

// DualIssueBatch 发 n 张本站码，按张原子；返回已成功的本站码。
func DualIssueBatch(ctx context.Context, plan string, n int, allowDegraded bool) ([]string, error) {
	if n < 1 {
		return nil, fmt.Errorf("count must be >= 1")
	}
	codes := make([]string, 0, n)
	for i := 0; i < n; i++ {
		res, err := DualIssueOne(ctx, plan, allowDegraded)
		if err != nil {
			// 已成功的几张保留（已激活、可用），未齐由调用方决定是否失败整单。
			return codes, err
		}
		codes = append(codes, res.SiteCode)
	}
	return codes, nil
}
