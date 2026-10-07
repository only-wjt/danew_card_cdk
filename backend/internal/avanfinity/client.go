// Package avanfinity 调用 Avanfinity Developer API v1（X-App-Id + X-App-Secret）。
// 这一层只服务 X 会员账户，不走 OpenAI 的 /openapi/v1。
package avanfinity

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultTimeout = 20 * time.Second

// Client 是一个 App 的调用方。Base 不含 /api/v1。
type Client struct {
	Base   string
	AppID  string
	Secret string
	HTTP   *http.Client
}

// APIError 保留 HTTP 状态和上游 errorCode，调用方按状态分支，不按文案。
type APIError struct {
	Status    int
	ErrorCode string
	Message   string
}

func (e *APIError) Error() string {
	if e.ErrorCode != "" {
		return fmt.Sprintf("avanfinity %d %s: %s", e.Status, e.ErrorCode, e.Message)
	}
	return fmt.Sprintf("avanfinity %d: %s", e.Status, e.Message)
}

type Balance struct {
	Balance      string `json:"balance"`
	TotalBalance string `json:"totalBalance"`
}

type XPlan struct {
	Key            string `json:"key"`
	Label          string `json:"label"`
	Tier           string `json:"tier"`
	Months         int    `json:"months"`
	Enabled        bool   `json:"enabled"`
	ServiceFee     string `json:"serviceFee"`
	PricingVersion int64  `json:"pricingVersion"`
}

type XPlans struct {
	Plans           []XPlan `json:"plans"`
	PaymentsEnabled bool    `json:"paymentsEnabled"`
}

type Card struct {
	ID               int64  `json:"id"`
	CardNumberMasked string `json:"cardNumberMasked"`
	ProductCode      string `json:"productCode"`
	Status           string `json:"status"`
	Balance          string `json:"balance"`
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: defaultTimeout}
}

func (c *Client) base() string {
	b := strings.TrimRight(strings.TrimSpace(c.Base), "/")
	b = strings.TrimSuffix(b, "/api/v1")
	b = strings.TrimSuffix(b, "/openapi/v1")
	b = strings.TrimSuffix(b, "/openapi")
	return b
}

// Call 发起一次请求。auth 为 false 时不带 App 凭证，给公开 X CDK 兑换用。
func (c *Client) Call(ctx context.Context, method, path, idem string, body any, out any, auth bool) error {
	return c.do(ctx, method, path, idem, body, out, auth)
}

func (c *Client) do(ctx context.Context, method, path, idem string, body any, out any, auth bool) error {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base()+path, rdr)
	if err != nil {
		return err
	}
	if auth {
		req.Header.Set("X-App-Id", strings.TrimSpace(c.AppID))
		req.Header.Set("X-App-Secret", strings.TrimSpace(c.Secret))
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{Status: resp.StatusCode, Message: strings.TrimSpace(string(raw))}
		var env struct {
			Error     string `json:"error"`
			ErrorCode string `json:"errorCode"`
		}
		if json.Unmarshal(raw, &env) == nil && env.Error != "" {
			apiErr.Message = env.Error
			apiErr.ErrorCode = env.ErrorCode
		}
		return apiErr
	}
	if out == nil {
		return nil
	}
	var env struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("avanfinity: 响应不是 JSON")
	}
	// /api/v1 成功码有 0 也有 200，两种都算成功
	if env.Code != 0 && env.Code != 200 {
		return fmt.Errorf("avanfinity: code=%d", env.Code)
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

func (c *Client) GetBalance(ctx context.Context) (*Balance, error) {
	var b Balance
	if err := c.do(ctx, http.MethodGet, "/api/v1/balance", "", nil, &b, true); err != nil {
		return nil, err
	}
	return &b, nil
}

func (c *Client) GetXPlans(ctx context.Context) (*XPlans, error) {
	var p XPlans
	if err := c.do(ctx, http.MethodGet, "/api/v1/x-direct/plans", "", nil, &p, true); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) ListCards(ctx context.Context) ([]Card, error) {
	var wrap struct {
		List []Card `json:"list"`
	}
	// 文档里 CardsResponse.data 可能直接是数组，也可能是 {list:[]}。两种都接。
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v1/cards?page=1&page_size=100", "", nil, &raw, true); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[0] == '[' {
		var list []Card
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, err
		}
		return list, nil
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	return wrap.List, nil
}

// ProbeWriteAllowlist 用一张已有卡生成限额极小的 X CDK，成功后立刻撤销。
// 自动开卡会真的开卡，所以没有可用卡时不探测。
func (c *Client) ProbeWriteAllowlist(ctx context.Context, cardID int64) (detail string, err error) {
	idem := newIdempotencyKey()
	body := map[string]any{
		"plan":                   "premium_3m",
		"quantity":               1,
		"maxWalletDebitUsd":      "0.0100",
		"maxOfficialAmountMinor": 1,
		"currency":               "usd",
		"fundingAmountUsd":       "0",
		"cardId":                 cardID,
	}
	// 文档里 X CDK 的 id 是 uuid 字符串，不是整数。
	var created struct {
		List []struct {
			ID         string `json:"id"`
			CodePrefix string `json:"codePrefix"`
		} `json:"list"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/x-direct/cdks/generate", idem, body, &created, true); err != nil {
		return "", err
	}
	if len(created.List) == 0 || strings.TrimSpace(created.List[0].ID) == "" {
		return "", fmt.Errorf("测试发码没有返回可撤销的 id")
	}
	id := strings.TrimSpace(created.List[0].ID)
	revokeIdem := newIdempotencyKey()
	path := "/api/v1/x-direct/cdks/" + url.PathEscape(id) + "/revoke"
	if err := c.do(ctx, http.MethodPost, path, revokeIdem, nil, nil, true); err != nil {
		return "", fmt.Errorf("测试码 %s（%s）已生成，但撤销失败，请到 Avanfinity 后台撤销: %w", id, created.List[0].CodePrefix, err)
	}
	return "通过 · 测试码已撤销", nil
}

// UsableCard 挑一张看起来还能付款的卡。冻结、关闭的跳过。
func newIdempotencyKey() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func UsableCard(cards []Card) (Card, bool) {
	for _, card := range cards {
		st := strings.ToLower(card.Status)
		if card.ID <= 0 {
			continue
		}
		if strings.Contains(st, "frozen") || strings.Contains(st, "closed") || strings.Contains(st, "deleted") || strings.Contains(st, "disabled") {
			continue
		}
		return card, true
	}
	return Card{}, false
}
