package provider

import (
	"context"
	"log"

	"github.com/danew/cdk-recharge-system/internal/db"
)

// RetireSiblings 本站码在某台核销成功后，把另一台那张未用的码收回。
// 删除退款优先（钱能回来）；上游不支持或拒绝就 disable 成 retired，
// 至少保证同一本站码不可能在两个台各兑一次。
func RetireSiblings(ctx context.Context, siteCodeID, consumedBindingID int64) {
	if siteCodeID <= 0 {
		return
	}
	siblings, err := db.SiblingBindings(siteCodeID, consumedBindingID)
	if err != nil {
		log.Printf("[retire] list siblings site_code_id=%d: %v", siteCodeID, err)
		return
	}
	for _, b := range siblings {
		p, _, perr := ForAccount(b.AccountID)
		if perr != nil {
			_ = db.UpdateBindingStatus(b.ID, db.BindingStatusUnknown, perr.Error())
			continue
		}
		if err := p.DeleteAndRefund(ctx, b.RemoteID); err == nil {
			_ = db.UpdateBindingStatus(b.ID, db.BindingStatusRefunded, "")
			continue
		} else if err != ErrRefundUnsupported {
			log.Printf("[retire] delete+refund failed binding=%d remote=%s: %v", b.ID, b.RemoteID, err)
		}
		if err := p.Disable(ctx, b.RemoteID); err != nil {
			// 收不回来是钱的问题，兑换侧靠 binding 状态兜底，所以标 unknown 等人工。
			_ = db.UpdateBindingStatus(b.ID, db.BindingStatusUnknown, err.Error())
			log.Printf("[retire] disable failed binding=%d remote=%s: %v", b.ID, b.RemoteID, err)
			continue
		}
		_ = db.UpdateBindingStatus(b.ID, db.BindingStatusRetired, "")
	}
}

// MarkConsumed 记录本站码在哪台核销成功，并异步退役兄弟码。
func MarkConsumed(ctx context.Context, r *Route) {
	if r == nil || !r.IsSite {
		return
	}
	_ = db.UpdateBindingStatus(r.BindingID, db.BindingStatusConsumed, "")
	_ = db.MarkSiteCDKFulfilled(r.SiteCodeID, r.Account.ID, r.Provider.Protocol(), r.FailoverUsed, r.FailoverReason)
	RetireSiblings(ctx, r.SiteCodeID, r.BindingID)
}
