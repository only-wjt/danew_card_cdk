package avanfinity

import (
	"context"
	"net/http"
	"strings"
)

// PublicCDK 是公开兑换返回的业务快照。HTTP 200 不代表付款完成。
// 发行者接口才有 fundingDispatched / paymentDispatched / estimatedUsd。
// 公开响应没有这些字段，缺省不能当成「没动钱」。
type PublicCDK struct {
	Plan              string   `json:"plan"`
	Status            string   `json:"status"`
	Recipient         string   `json:"recipient"`
	AmountMinor       int64    `json:"amountMinor"`
	Currency          string   `json:"currency"`
	Message           string   `json:"message"`
	ErrorCode         string   `json:"errorCode"`
	InvoiceURLs       []string `json:"invoiceUrls"`
	CanRetryPreflight *bool    `json:"canRetryPreflight"`
}

// DirectOrder 是直充订单快照。
type DirectOrder struct {
	ID               string   `json:"id"`
	Status           string   `json:"status"`
	Recipient        string   `json:"recipient"`
	Plan             string   `json:"plan"`
	AmountMinor      int64    `json:"amountMinor"`
	Currency         string   `json:"currency"`
	EstimatedUSD     string   `json:"estimatedUsd"`
	ServiceFee       string   `json:"serviceFee"`
	PricingVersion   int64    `json:"pricingVersion"`
	PaymentAttempted bool     `json:"paymentAttempted"`
	InvoiceURLs      []string `json:"invoiceUrls"`
	ErrorCode        string   `json:"errorCode"`
	Message          string   `json:"message"`

	CardID              int64   `json:"cardId"`
	ServiceFeeStatus    string  `json:"serviceFeeStatus"`
	PaymentSubmitted    bool    `json:"paymentSubmitted"`
	PaymentStatus       *string `json:"paymentStatus"`
	PaymentIntentStatus *string `json:"paymentIntentStatus"`
	CardLast4           string  `json:"cardLast4"`
	FxRateDate          *string `json:"fxRateDate"`
	EligibleBefore      bool    `json:"eligibleBefore"`
	EligibleAfter       bool    `json:"eligibleAfter"`
	Product             string  `json:"product"`
	CreatedAt           string  `json:"createdAt"`
}

// IssuedCDK 是生成结果里的一张上游码。Code 只在内存里停留，调用方必须立刻加密。
type IssuedCDK struct {
	ID                     string `json:"id"`
	Code                   string `json:"code"`
	CodePrefix             string `json:"codePrefix"`
	Plan                   string `json:"plan"`
	Currency               string `json:"currency"`
	MaxOfficialAmountMinor int64  `json:"maxOfficialAmountMinor"`
	MaxWalletDebitUSD      string `json:"maxWalletDebitUsd"`
	FundingAmountUSD       string `json:"fundingAmountUsd"`
	ServiceFee             string `json:"serviceFee"`
	PricingVersion         int64  `json:"pricingVersion"`

	Status              string  `json:"status"`
	TotalWalletDebitUSD string  `json:"totalWalletDebitUsd"`
	CardID              *int64  `json:"cardId"`
	Recipient           *string `json:"recipient"`
	FundingDispatched   bool    `json:"fundingDispatched"`
	PaymentDispatched   bool    `json:"paymentDispatched"`
	FundingFeeUSD       string  `json:"fundingFeeUsd"`
	OpenFeeUSD          string  `json:"openFeeUsd"`
	CreatedAt           string  `json:"createdAt"`
}

type GenerateResult struct {
	List     []IssuedCDK `json:"list"`
	Replayed bool        `json:"replayed"`
}

func (c *Client) GenerateCDKs(ctx context.Context, idem string, body map[string]any) (*GenerateResult, error) {
	var out GenerateResult
	if err := c.Call(ctx, http.MethodPost, "/api/v1/x-direct/cdks/generate", idem, body, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RevokeCDK(ctx context.Context, id, idem string) error {
	return c.Call(ctx, http.MethodPost, "/api/v1/x-direct/cdks/"+id+"/revoke", idem, nil, nil, true)
}

func (c *Client) PreviewCDK(ctx context.Context, code, deviceToken string) (*PublicCDK, error) {
	var out PublicCDK
	err := c.Call(ctx, http.MethodPost, "/api/v1/public/x-cdk/preview", "", map[string]string{
		"code": code, "deviceToken": deviceToken,
	}, &out, false)
	return &out, err
}

func (c *Client) PreflightCDK(ctx context.Context, code, deviceToken, recipient, clientRequestID string) (*PublicCDK, error) {
	var out PublicCDK
	err := c.Call(ctx, http.MethodPost, "/api/v1/public/x-cdk/preflight", "", map[string]string{
		"code": code, "deviceToken": deviceToken, "recipient": recipient, "clientRequestId": clientRequestID,
	}, &out, false)
	return &out, err
}

func (c *Client) RedeemCDK(ctx context.Context, code, deviceToken, clientRequestID, currency string, amountMinor int64) (*PublicCDK, error) {
	// 文档要求 expectedAmountMinor 和 currency（小写三位）必填，缺了上游直接 400。
	body := map[string]any{
		"code": code, "deviceToken": deviceToken, "clientRequestId": clientRequestID,
		"expectedAmountMinor": amountMinor,
		"currency":            strings.ToLower(strings.TrimSpace(currency)),
	}
	var out PublicCDK
	err := c.Call(ctx, http.MethodPost, "/api/v1/public/x-cdk/redeem", "", body, &out, false)
	return &out, err
}

func (c *Client) ResultCDK(ctx context.Context, code, deviceToken, clientRequestID string) (*PublicCDK, error) {
	var out PublicCDK
	err := c.Call(ctx, http.MethodPost, "/api/v1/public/x-cdk/result", "", map[string]string{
		"code": code, "deviceToken": deviceToken, "clientRequestId": clientRequestID,
	}, &out, false)
	return &out, err
}

func (c *Client) PrepareDirect(ctx context.Context, idem string, body map[string]any) (*DirectOrder, error) {
	var out DirectOrder
	err := c.Call(ctx, http.MethodPost, "/api/v1/x-direct/orders/prepare", idem, body, &out, true)
	return &out, err
}

func (c *Client) ConfirmDirect(ctx context.Context, orderID, idem string, amountMinor int64, currency, serviceFee string) (*DirectOrder, error) {
	var out DirectOrder
	err := c.Call(ctx, http.MethodPost, "/api/v1/x-direct/orders/"+orderID+"/confirm", idem, map[string]any{
		"expectedAmountMinor": amountMinor,
		"currency":            currency,
		"expectedServiceFee":  serviceFee,
	}, &out, true)
	return &out, err
}

func (c *Client) CancelDirect(ctx context.Context, orderID, idem string) error {
	return c.Call(ctx, http.MethodPost, "/api/v1/x-direct/orders/"+orderID+"/cancel", idem, nil, nil, true)
}

func (c *Client) ExportCDK(ctx context.Context, id string) (string, error) {
	var out struct {
		Code string `json:"code"`
	}
	if err := c.Call(ctx, http.MethodGet, "/api/v1/x-direct/cdks/"+id+"/export", "", nil, &out, true); err != nil {
		return "", err
	}
	return out.Code, nil
}

func (c *Client) GetDirectOrder(ctx context.Context, orderID string) (*DirectOrder, error) {
	var out DirectOrder
	err := c.Call(ctx, http.MethodGet, "/api/v1/x-direct/orders/"+orderID, "", nil, &out, true)
	return &out, err
}
