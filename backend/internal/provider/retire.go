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
			// 不是本次该处理的（别人正在收，或状态不适合自动收回）。
			// 打日志而不是静默跳过：reserved/unknown 这类需要人工确认
			// 「上游那张码是不是还活着」（review Medium-3）。
			log.Printf("[retire] skip sibling binding=%d status=%s remote=%s", b.ID, b.Status, b.RemoteID)
			continue
		}
		RetireOne(ctx, b)
	}
}

// RetireOne 收回单张已认领的 binding（调用方必须已用 ClaimBindingForRetire 拿到所有权）。
// 抽出来是为了让管理端「重试收回」能复用同一条链路。
func RetireOne(ctx context.Context, b db.SiteCDKBinding) {
	p, _, perr := ForAccount(b.AccountID)
	if perr != nil {
		_ = db.UpdateBindingStatus(b.ID, db.BindingStatusUnknown, perr.Error())
		return
	}
	if err := p.DeleteAndRefund(ctx, b.RemoteID); err == nil {
		_, _ = db.UpdateBindingStatusFrom(b.ID, []string{db.BindingStatusRetiring}, db.BindingStatusRefunded, "")
		return
	} else if err != ErrRefundUnsupported {
		log.Printf("[retire] delete+refund failed binding=%d remote=%s: %v", b.ID, b.RemoteID, err)
	}
	if err := p.Disable(ctx, b.RemoteID); err == ErrRefundUnsupported {
		// 上游没有作废接口（Avanfinity）：只在本站标作废，路由不会再选这张。
		_ = db.UpdateBindingStatus(b.ID, db.BindingStatusRetired, "上游不支持作废，仅本站标记")
		return
	} else if err != nil {
		// 收不回来是钱的问题，兑换侧靠 binding 状态兜底，所以标 unknown 等人工。
		_ = db.UpdateBindingStatus(b.ID, db.BindingStatusUnknown, err.Error())
		log.Printf("[retire] disable failed binding=%d remote=%s: %v", b.ID, b.RemoteID, err)
		return
	}
	_ = db.UpdateBindingStatus(b.ID, db.BindingStatusRetired, "")
}

// MarkConsumed 记录本站码在哪台核销成功，并退役兄弟码。
//
// 条件转换保证只有第一个调用者生效：result 轮询（每 3 秒一次、可能多个标签页）
// 和 webhook 都会调到这里，无条件写入会让兄弟码被退役多次。
func MarkConsumed(ctx context.Context, r *Route) {
	if r == nil || !r.IsSite {
		return
	}
	won, err := db.MarkBindingConsumed(r.BindingID)
	if err != nil {
		log.Printf("[retire] mark consumed binding=%d: %v", r.BindingID, err)
		return
	}
	if won {
		_ = db.MarkSiteCDKFulfilled(r.SiteCodeID, r.Account.ID, r.Provider.Protocol(), r.FailoverUsed, r.FailoverReason)
	}
	// 无论是不是本次写成的都继续退役兄弟码：RetireSiblings 自己会先认领每张兄弟码，
	// 重复调用是幂等的。条件写只用来决定「谁记录履约信息」，不能用来决定
	// 「要不要退役」——否则 unknown/其它状态的兄弟码会被永久漏掉（review #5）。
	RetireSiblings(ctx, r.SiteCodeID, r.BindingID)
}
