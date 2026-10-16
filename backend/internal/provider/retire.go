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
		// 先认领再动上游：同一本站码会被多路轮询（result 页、webhook）反复触发，
		// 不认领的话两张 goroutine 会同时对同一张码退款，后到的还会把 refunded
		// 覆盖成 unknown，误触发人工资金复核（见 C8）。
		claimed, cerr := db.ClaimBindingForRetire(b.ID)
		if cerr != nil {
			log.Printf("[retire] claim binding=%d: %v", b.ID, cerr)
			continue
		}
		if !claimed {
			continue
		}
		p, _, perr := ForAccount(b.AccountID)
		if perr != nil {
			_ = db.UpdateBindingStatus(b.ID, db.BindingStatusUnknown, perr.Error())
			continue
		}
		if err := p.DeleteAndRefund(ctx, b.RemoteID); err == nil {
			_, _ = db.UpdateBindingStatusFrom(b.ID, []string{db.BindingStatusRetiring}, db.BindingStatusRefunded, "")
			continue
		} else if err != ErrRefundUnsupported {
			log.Printf("[retire] delete+refund failed binding=%d remote=%s: %v", b.ID, b.RemoteID, err)
		}
		if err := p.Disable(ctx, b.RemoteID); err == ErrRefundUnsupported {
			// 上游没有作废接口（Avanfinity）：只在本站标作废，路由不会再选这张。
			_ = db.UpdateBindingStatus(b.ID, db.BindingStatusRetired, "上游不支持作废，仅本站标记")
			continue
		} else if err != nil {
			// 收不回来是钱的问题，兑换侧靠 binding 状态兜底，所以标 unknown 等人工。
			_ = db.UpdateBindingStatus(b.ID, db.BindingStatusUnknown, err.Error())
			log.Printf("[retire] disable failed binding=%d remote=%s: %v", b.ID, b.RemoteID, err)
			continue
		}
		_ = db.UpdateBindingStatus(b.ID, db.BindingStatusRetired, "")
	}
}

// MarkConsumed 记录本站码在哪台核销成功，并退役兄弟码。
//
// 条件转换保证只有第一个调用者生效：result 轮询（每 3 秒一次、可能多个标签页）
// 和 webhook 都会调到这里，无条件写入会让兄弟码被退役多次。
func MarkConsumed(ctx context.Context, r *Route) {
	if r == nil || !r.IsSite {
		return
	}
	won, err := db.UpdateBindingStatusFrom(r.BindingID,
		[]string{db.BindingStatusRedeeming, db.BindingStatusUnused, db.BindingStatusFailed, db.BindingStatusReserved},
		db.BindingStatusConsumed, "")
	if err != nil {
		log.Printf("[retire] mark consumed binding=%d: %v", r.BindingID, err)
		return
	}
	if !won {
		// 已经是 consumed（或已退役/退回），说明另一次调用已经在处理，不重复退役。
		return
	}
	_ = db.MarkSiteCDKFulfilled(r.SiteCodeID, r.Account.ID, r.Provider.Protocol(), r.FailoverUsed, r.FailoverReason)
	RetireSiblings(ctx, r.SiteCodeID, r.BindingID)
}
