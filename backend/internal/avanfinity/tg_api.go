package avanfinity

import (
	"context"
	"net/http"
	"strings"
)

// TG 和 X 的 CDK 形状一样。公开接口在 /api/public/tg-cdk/*，发码在 /api/v1/tg-direct。

func (c *Client) GenerateTGCDKs(ctx context.Context, idem string, body map[string]any) (*GenerateResult, error) {
	var out GenerateResult
	if err := c.Call(ctx, http.MethodPost, "/api/v1/tg-direct/cdks/generate", idem, body, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RevokeTGCDK(ctx context.Context, id, idem string) error {
	return c.Call(ctx, http.MethodPost, "/api/v1/tg-direct/cdks/"+id+"/revoke", idem, nil, nil, true)
}

func (c *Client) ExportTGCDK(ctx context.Context, id string) (string, error) {
	var out struct {
		Code string `json:"code"`
	}
	if err := c.Call(ctx, http.MethodGet, "/api/v1/tg-direct/cdks/"+id+"/export", "", nil, &out, true); err != nil {
		return "", err
	}
	return out.Code, nil
}

func (c *Client) PreviewTGCDK(ctx context.Context, code, deviceToken string) (*PublicCDK, error) {
	var out PublicCDK
	err := c.Call(ctx, http.MethodPost, "/api/public/tg-cdk/preview", "", map[string]string{
		"code": code, "deviceToken": deviceToken,
	}, &out, false)
	return &out, err
}

func (c *Client) PreflightTGCDK(ctx context.Context, code, deviceToken, recipient, clientRequestID string) (*PublicCDK, error) {
	var out PublicCDK
	err := c.Call(ctx, http.MethodPost, "/api/public/tg-cdk/preflight", "", map[string]string{
		"code": code, "deviceToken": deviceToken, "recipient": recipient, "clientRequestId": clientRequestID,
	}, &out, false)
	return &out, err
}

func (c *Client) RedeemTGCDK(ctx context.Context, code, deviceToken, clientRequestID, currency string, amountMinor int64) (*PublicCDK, error) {
	body := map[string]any{
		"code": code, "deviceToken": deviceToken, "clientRequestId": clientRequestID,
		"expectedAmountMinor": amountMinor,
		"currency":            strings.ToLower(strings.TrimSpace(currency)),
	}
	var out PublicCDK
	err := c.Call(ctx, http.MethodPost, "/api/public/tg-cdk/redeem", "", body, &out, false)
	return &out, err
}

func (c *Client) ResultTGCDK(ctx context.Context, code, deviceToken, clientRequestID string) (*PublicCDK, error) {
	var out PublicCDK
	err := c.Call(ctx, http.MethodPost, "/api/public/tg-cdk/result", "", map[string]string{
		"code": code, "deviceToken": deviceToken, "clientRequestId": clientRequestID,
	}, &out, false)
	return &out, err
}
