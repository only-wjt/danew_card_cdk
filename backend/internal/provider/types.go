package provider

import "context"

// Protocol 标识卡台 API 适配器类型。
const (
	ProtocolSpaceXLegacy     = "spacexcard-legacy"
	ProtocolAvanfinity202608 = "avanfinity-2026-08"
)

// SiteCodePrefix 对外本站码前缀（与 GPTD-/SXC-/AVF- 区分）。
const SiteCodePrefix = "DN-"

// IssuedUpstream 单张上游发码结果（完整码只在 Issue 响应里，须立刻落库）。
type IssuedUpstream struct {
	RemoteID    string
	RemoteCode  string
	CodePrefix  string
	Plan        string
	FeeMinor    int64
	Idempotency string
}

// IssuePreference 是某一个卡台账户自己的选卡映射；A/B 的产品码可以不同。
type IssuePreference struct {
	Issuer      string
	SegmentType string
	SegmentKey  string
}

// CardProvider 双卡台 adapter 接口。
//
// 兑换四步（Preview/Preflight/Redeem/Result）都带 remoteCode 或 token：
// 上游只认自己发的码，本站码必须在这层翻译掉。
type CardProvider interface {
	Protocol() string
	AccountID() int64
	IssueCDK(ctx context.Context, plan string, idem string, pref IssuePreference) (*IssuedUpstream, error)
	Preview(ctx context.Context, remoteCode, device string) (status int, raw []byte, err error)
	Preflight(ctx context.Context, body map[string]any, device string) (status int, raw []byte, err error)
	Redeem(ctx context.Context, body map[string]any, device string) (status int, raw []byte, err error)
	Result(ctx context.Context, token, device string) (status int, raw []byte, err error)
	Disable(ctx context.Context, remoteID string) error
	DeleteAndRefund(ctx context.Context, remoteID string) error
}
