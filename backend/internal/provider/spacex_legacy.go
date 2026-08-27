package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
	"github.com/danew/cdk-recharge-system/internal/db"
)

// ErrRefundUnsupported 该协议没有「删除即退款」能力，调用方应降级 Disable。
var ErrRefundUnsupported = errors.New("provider does not support delete-and-refund")

// spacexLegacy 现网 SpaceX 风格 OpenAPI（/openapi/v1 + X-API-Key）。
type spacexLegacy struct {
	account db.CardPlatformAccount
	client  *cardplatform.Client
}

// NewSpaceXLegacy 用账户凭证构造 adapter（不读全局 settings，多账户才能并存）。
func NewSpaceXLegacy(acc db.CardPlatformAccount) CardProvider {
	base := strings.TrimRight(strings.TrimSpace(acc.SiteBase), "/")
	base = strings.TrimSuffix(base, "/openapi/v1")
	base = strings.TrimSuffix(base, "/openapi")
	return &spacexLegacy{
		account: acc,
		client: cardplatform.New(cardplatform.Config{
			SiteBase: base,
			APIKey:   strings.TrimSpace(acc.CredSecret),
		}),
	}
}

func (p *spacexLegacy) Protocol() string { return ProtocolSpaceXLegacy }
func (p *spacexLegacy) AccountID() int64 { return p.account.ID }

func (p *spacexLegacy) IssueCDK(ctx context.Context, plan string, idem string, pref IssuePreference) (*IssuedUpstream, error) {
	var res *cardplatform.IssueCDKResult
	var err error
	if strings.TrimSpace(pref.SegmentKey) != "" || strings.TrimSpace(pref.Issuer) != "" {
		res, err = p.client.IssueCDKs(ctx, plan, 1, idem, cardplatform.IssueCardPref{
			Issuer: pref.Issuer, SegmentType: pref.SegmentType, SegmentKey: pref.SegmentKey,
		})
	} else {
		res, err = p.client.IssueCDKs(ctx, plan, 1, idem)
	}
	if err != nil {
		return nil, err
	}
	if res == nil || len(res.Issued) == 0 {
		return nil, fmt.Errorf("上游未返回卡密")
	}
	it := res.Issued[0]
	code := strings.TrimSpace(it.Code)
	if code == "" {
		// 完整码只在发码响应里出现，拿不到就必须当失败，否则这张永远无法兑换。
		return nil, fmt.Errorf("上游未返回完整卡密（id=%d）", it.ID)
	}
	prefix := strings.TrimSpace(it.CodePrefix)
	if prefix == "" && len(code) >= 14 {
		prefix = code[:14]
	}
	return &IssuedUpstream{
		RemoteID:    strconv.FormatInt(it.ID, 10),
		RemoteCode:  code,
		CodePrefix:  prefix,
		Plan:        strings.TrimSpace(it.Plan),
		FeeMinor:    it.FeeAmountMinor,
		Idempotency: idem,
	}, nil
}

func (p *spacexLegacy) Preview(ctx context.Context, remoteCode, device string) (int, []byte, error) {
	st, raw, err := p.client.Preview(ctx, remoteCode, device)
	return st, raw, err
}

func (p *spacexLegacy) Preflight(ctx context.Context, body map[string]any, device string) (int, []byte, error) {
	st, raw, err := p.client.Preflight(ctx, body, device)
	return st, raw, err
}

func (p *spacexLegacy) Redeem(ctx context.Context, body map[string]any, device string) (int, []byte, error) {
	st, raw, err := p.client.Redeem(ctx, body, device)
	return st, raw, err
}

func (p *spacexLegacy) Result(ctx context.Context, token, device string) (int, []byte, error) {
	st, raw, err := p.client.Result(ctx, token, device)
	return st, raw, err
}

func (p *spacexLegacy) Disable(ctx context.Context, remoteID string) error {
	id, err := strconv.ParseInt(strings.TrimSpace(remoteID), 10, 64)
	if err != nil {
		return fmt.Errorf("invalid remote id %q", remoteID)
	}
	return p.client.DisableCDK(ctx, id)
}

// DeleteAndRefund 旧台「删除并退款」：未使用的购买码删除后自动退回服务费。
// 上游没开放该 OpenAPI 路径时返回 ErrRefundUnsupported，调用方降级为 Disable。
func (p *spacexLegacy) DeleteAndRefund(ctx context.Context, remoteID string) error {
	id, err := strconv.ParseInt(strings.TrimSpace(remoteID), 10, 64)
	if err != nil {
		return fmt.Errorf("invalid remote id %q", remoteID)
	}
	err = p.client.DeleteCDK(ctx, id)
	if err == nil {
		return nil
	}
	var apiErr *cardplatform.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.HTTPStatus {
		case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
			return ErrRefundUnsupported
		}
	}
	return err
}
