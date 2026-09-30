// Package xlogic 是 X 会员里不碰数据库、也不打上游的判断。
// 轮询能不能付款、码能不能放回、报价失败该重试还是锁住，都在这里，方便不连库也能测。
package xlogic

import (
	"strings"
	"time"
)

const (
	ActionConfirm = "confirm"
	ActionQuote   = "quote"
	ActionRefresh = "refresh"
	ActionIdle    = "idle"

	FailRetry     = "retry"
	FailWait      = "wait"
	FailUncertain = "uncertain"
	FailBalance   = "balance"
	FailStale     = "stale"
	FailReturn    = "return"

	ActReuse   = "reuse"
	ActReplace = "replace"
	ActReject  = "reject"
)

// PollAction 决定后台轮询对这条兑换做什么。
// 只有客户点过确认（queued_confirm）才能进入付款。报价成功本身不能付款。
func PollAction(codeStatus, upstreamStatus, orderID string) string {
	switch strings.TrimSpace(upstreamStatus) {
	case "queued_confirm":
		return ActionConfirm
	case "queued_quote":
		return ActionQuote
	}
	if codeStatus == "unused" {
		return ActionQuote
	}
	// 已报价、还没进入付款：停在这里等客户确认。不要因为有提示文案就去 redeem。
	if codeStatus == "quoted" && strings.TrimSpace(orderID) == "" {
		switch strings.TrimSpace(upstreamStatus) {
		case "", "prepared", "preparing":
			return ActionIdle
		}
	}
	if codeStatus == "quoted" && strings.TrimSpace(upstreamStatus) == "prepared" {
		return ActionIdle
	}
	return ActionRefresh
}

// ShouldRelease 只有设计里写明的情况可以把码放回未使用。
// canRetry 必须严格为 true。ineligible 只在还没发起付款、也没注资时放回。
func ShouldRelease(upstream string, paymentAttempted, fundingDispatched bool, canRetry *bool) bool {
	if canRetry != nil && *canRetry {
		return true
	}
	if paymentAttempted || fundingDispatched {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(upstream)) {
	case "ineligible", "cancelled":
		return true
	default:
		return false
	}
}

// RecipientAction：同一笔未完成的兑换，收款人变了就要作废旧报价并换新的请求号。
// 已经动过钱则拒绝改账号。
func RecipientAction(current, next string, moneyMoved bool) string {
	current = strings.TrimPrefix(strings.TrimSpace(current), "@")
	next = strings.TrimPrefix(strings.TrimSpace(next), "@")
	if current == "" || strings.EqualFold(current, next) {
		return ActReuse
	}
	if moneyMoved {
		return ActReject
	}
	return ActReplace
}

func QuoteStale(errorCode string) bool {
	switch strings.ToUpper(strings.TrimSpace(errorCode)) {
	case "QUOTE_EXPIRED", "QUOTE_CHANGED":
		return true
	default:
		return false
	}
}

// ClassifyFailure 按阶段区分失败。
// quote / read 还没动钱，网络错误用原请求重试。
// pay 的网络错误或 5xx 视为结果不确定，只查询，不另建订单。
func ClassifyFailure(stage string, network bool, httpStatus int, errorCode string) string {
	if QuoteStale(errorCode) {
		return FailStale
	}
	if errorCode == "SPENDABLE_BALANCE_INSUFFICIENT" {
		return FailBalance
	}
	if network || httpStatus == 408 || httpStatus >= 500 {
		if stage == "pay" {
			return FailUncertain
		}
		return FailRetry
	}
	if httpStatus == 429 {
		return FailWait
	}
	return FailReturn
}

// StatusDecision 是上游状态落到本站码上的结果。
type StatusDecision struct {
	CodeStatus string
	Finish     bool
	Poll       bool
	Release    bool
}

// DecideStatus 显式映射上游每一个状态。公开 CDK 响应没有 funding/payment 标志，
// 这两个布尔值只能来自本站自己记下的「已经发出过 redeem / confirm」。
func DecideStatus(prev, upstream string, paymentAttempted, fundingDispatched bool, canRetry *bool) StatusDecision {
	upstream = strings.ToLower(strings.TrimSpace(upstream))
	if upstream != "" && ShouldRelease(upstream, paymentAttempted, fundingDispatched, canRetry) {
		return StatusDecision{CodeStatus: "unused", Finish: true, Release: true}
	}
	switch upstream {
	case "prepared":
		return StatusDecision{CodeStatus: "quoted"}
	case "preparing":
		if paymentAttempted || fundingDispatched {
			return StatusDecision{CodeStatus: "funding", Poll: true}
		}
		return StatusDecision{CodeStatus: "quoted", Poll: true}
	case "funding":
		return StatusDecision{CodeStatus: "funding", Poll: true}
	case "funded":
		return StatusDecision{CodeStatus: "funded", Poll: true}
	case "processing", "paying":
		return StatusDecision{CodeStatus: "paying", Poll: true}
	case "completed":
		return StatusDecision{CodeStatus: "completed", Finish: true}
	case "paid_pending_delivery":
		return StatusDecision{CodeStatus: "paid_pending_delivery", Poll: true}
	case "review_required":
		return StatusDecision{CodeStatus: "review_required", Poll: true}
	case "requires_action":
		return StatusDecision{CodeStatus: "requires_action", Poll: true}
	case "revoked":
		return StatusDecision{CodeStatus: "disabled", Finish: true}
	case "ineligible", "failed_precharge", "cancelled":
		return StatusDecision{CodeStatus: "uncertain", Poll: true}
	case "unused":
		if paymentAttempted || fundingDispatched {
			return StatusDecision{CodeStatus: "uncertain", Poll: true}
		}
		return StatusDecision{CodeStatus: "unused", Finish: true}
	case "":
		if prev == "" {
			prev = "unused"
		}
		return StatusDecision{CodeStatus: prev}
	default:
		return StatusDecision{CodeStatus: "uncertain", Poll: true}
	}
}

// ShouldSecondRedeem 只在 CDK 已经注资、且本站还没发出第二次 redeem 时为真。
func ShouldSecondRedeem(channel, codeStatus string, paymentDispatched bool) bool {
	return channel == "x_cdk" && codeStatus == "funded" && !paymentDispatched
}

// NoteFunding 在真正调用过 redeem 之后，用上游状态记下「钱可能已经动了」。
// 公开接口不返回 fundingDispatched，不能靠缺省的 false。
func NoteFunding(status string, canRetry *bool) (funding, payment bool) {
	if canRetry != nil && *canRetry {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "funding", "funded", "paying", "processing", "completed", "paid_pending_delivery", "review_required", "requires_action", "failed_precharge", "ineligible":
		return true, true
	default:
		return false, false
	}
}

// AllowRate 是固定窗口计数。第 limit+1 次返回 false，并保留窗口内的时间戳。
func AllowRate(hits []time.Time, now time.Time, limit int, window time.Duration) (bool, []time.Time) {
	if limit < 1 {
		limit = 1
	}
	cut := now.Add(-window)
	kept := make([]time.Time, 0, len(hits)+1)
	for _, t := range hits {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= limit {
		return false, kept
	}
	return true, append(kept, now)
}
