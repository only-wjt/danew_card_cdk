package provider

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/danew/cdk-recharge-system/internal/db"
)

// avanfinityV2026 按 Avanfinity OpenAPI（2026-08）实现 GPT 备台：
// 发码 /api/v1/gpt-direct/cdks/generate（X-App-Id + X-App-Secret），
// 兑换 /api/v1/public/cdk/*（不带凭证，必须带 X-Redemption-Device UUID）。
// 文档没有作废/删除 CDK 的接口，回收只能在本站标记。
type avanfinityV2026 struct {
	account db.CardPlatformAccount
	base    string
	http    *http.Client
}

const avanfinityV2026Timeout = 30 * time.Second

// NewAvanfinityV2026 用账户凭证构造 adapter。CredPublic=App ID，CredSecret=App Secret。
func NewAvanfinityV2026(acc db.CardPlatformAccount) CardProvider {
	return &avanfinityV2026{
		account: acc,
		base:    avanfinityTrimBase(acc.SiteBase),
		http:    &http.Client{Timeout: avanfinityV2026Timeout},
	}
}

func avanfinityTrimBase(raw string) string {
	b := strings.TrimRight(strings.TrimSpace(raw), "/")
	for _, s := range []string{"/openapi/v1", "/openapi", "/api/v1"} {
		b = strings.TrimSuffix(b, s)
	}
	return b
}

func (p *avanfinityV2026) Protocol() string { return ProtocolAvanfinity202608 }
func (p *avanfinityV2026) AccountID() int64 { return p.account.ID }

// AvanfinityAPIError 保留上游 errorCode，调用方按代码分支，不按文案。
type AvanfinityAPIError struct {
	Status    int
	ErrorCode string
	Message   string
}

// avanfinityErrHints 按文档 errorCode 给运营看的说明，文案以代码为准。
var avanfinityErrHints = map[string]string{
	"SPENDABLE_BALANCE_INSUFFICIENT": "备台可用余额不足（风控锁定和重试预留不可用）",
	"WAITING_QUOTE":                  "备台该套餐暂未报价，稍后再发",
	"PRICE_CHANGED":                  "备台价格已变化，请重新发码",
	"PREFLIGHT_REQUIRED":             "需要先完成预检",
}

func (e *AvanfinityAPIError) Error() string {
	if h, ok := avanfinityErrHints[e.ErrorCode]; ok {
		return fmt.Sprintf("%s（%s）", h, e.ErrorCode)
	}
	if e.ErrorCode != "" {
		return fmt.Sprintf("avanfinity %d %s: %s", e.Status, e.ErrorCode, e.Message)
	}
	return fmt.Sprintf("avanfinity %d: %s", e.Status, e.Message)
}

// send 发请求并返回状态码与原始响应体。auth=true 时带 App 凭证。
func (p *avanfinityV2026) send(ctx context.Context, method, path string, body any, headers map[string]string, auth bool) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.base+"/api/v1"+path, rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth {
		req.Header.Set("X-App-Id", strings.TrimSpace(p.account.CredPublic))
		req.Header.Set("X-App-Secret", strings.TrimSpace(p.account.CredSecret))
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, raw, nil
}

// call 走带凭证的开发者接口，失败时返回 *AvanfinityAPIError。
func (p *avanfinityV2026) call(ctx context.Context, method, path string, body any, headers map[string]string, out any) error {
	st, raw, err := p.send(ctx, method, path, body, headers, true)
	if err != nil {
		return err
	}
	var env struct {
		Code      *int            `json:"code"`
		Data      json.RawMessage `json:"data"`
		Error     string          `json:"error"`
		ErrorCode string          `json:"errorCode"`
		Message   string          `json:"message"`
	}
	_ = json.Unmarshal(raw, &env)
	if st < 200 || st >= 300 || (env.Code != nil && *env.Code != 0) || env.ErrorCode != "" {
		msg := strings.TrimSpace(env.Error)
		if msg == "" {
			msg = strings.TrimSpace(env.Message)
		}
		if msg == "" {
			msg = http.StatusText(st)
		}
		return &AvanfinityAPIError{Status: st, ErrorCode: env.ErrorCode, Message: msg}
	}
	if out != nil && len(env.Data) > 0 {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

var avanfinityIdemRe = regexp.MustCompile(`^[\x21\x23-\x7E]{8,128}$`)

func avanfinityIdemKey(idem string) string {
	k := strings.TrimSpace(idem)
	if avanfinityIdemRe.MatchString(k) {
		return k
	}
	if k != "" {
		// 不合规的 key 也要稳定映射，重试才不会重复扣款。
		sum := sha1.Sum([]byte(k))
		return "idem-" + hex.EncodeToString(sum[:])
	}
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "idem-" + hex.EncodeToString(b[:])
}

// avanfinityPlan 本站套餐名 → 文档枚举 go|plus|pro_5x|pro_20x|pro_25x。
func avanfinityPlan(plan string) string {
	p := strings.ToLower(strings.TrimSpace(plan))
	p = strings.TrimPrefix(p, "gpt_")
	for _, suf := range []string{"_1m", "_monthly"} {
		p = strings.TrimSuffix(p, suf)
	}
	if p == "pro" {
		return "pro_20x"
	}
	return p
}

type avanfinityGenerated struct {
	SaleID     string  `json:"saleId"`
	CDKID      string  `json:"cdkId"`
	Code       *string `json:"code"`
	CodePrefix string  `json:"codePrefix"`
	Plan       string  `json:"plan"`
	SalePrice  string  `json:"salePrice"`
	Replayed   bool    `json:"replayed"`
}

func (p *avanfinityV2026) IssueCDK(ctx context.Context, plan string, idem string, _ IssuePreference) (*IssuedUpstream, error) {
	key := avanfinityIdemKey(idem)
	body := map[string]any{
		"plan":         avanfinityPlan(plan),
		"quantity":     1,
		"forceNewCard": p.account.ForceNewCard,
	}
	var out avanfinityGenerated
	if err := p.call(ctx, http.MethodPost, "/gpt-direct/cdks/generate", body, map[string]string{"Idempotency-Key": key}, &out); err != nil {
		return nil, err
	}
	code := ""
	if out.Code != nil {
		code = strings.TrimSpace(*out.Code)
	}
	if code == "" {
		// 完整码只在发码响应里出现（重放时可能为 null），拿不到就当失败。
		return nil, fmt.Errorf("上游未返回完整卡密（cdkId=%s）", out.CDKID)
	}
	prefix := strings.TrimSpace(out.CodePrefix)
	if prefix == "" && len(code) >= 12 {
		prefix = code[:12]
	}
	return &IssuedUpstream{
		RemoteID:    strings.TrimSpace(out.CDKID),
		RemoteCode:  code,
		CodePrefix:  prefix,
		Plan:        strings.TrimSpace(out.Plan),
		FeeMinor:    decimalToMinor(out.SalePrice),
		Idempotency: key,
	}, nil
}

func decimalToMinor(s string) int64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return int64(math.Round(f * 100))
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// avanfinityDevice 上游要求 UUID。浏览器没传时 deviceFrom 会退回 User-Agent，
// 这里按内容推导固定 UUID，保证同一设备四步一致。
func avanfinityDevice(device string) string {
	d := strings.TrimSpace(device)
	if uuidRe.MatchString(d) {
		return strings.ToLower(d)
	}
	sum := sha1.Sum([]byte("danew-device:" + d))
	return formatUUID(sum[:16], 5)
}

func newUUID4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return formatUUID(b[:], 4)
}

func formatUUID(b []byte, version byte) string {
	u := make([]byte, 16)
	copy(u, b)
	u[6] = (u[6] & 0x0f) | (version << 4)
	u[8] = (u[8] & 0x3f) | 0x80
	h := hex.EncodeToString(u)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func (p *avanfinityV2026) public(ctx context.Context, step string, body map[string]any, device string) (int, []byte, error) {
	st, raw, err := p.send(ctx, http.MethodPost, "/public/cdk/"+step, body,
		map[string]string{"X-Redemption-Device": avanfinityDevice(device)}, false)
	if err != nil {
		return st, raw, err
	}
	return st, addSnakeAliases(raw), nil
}

func pickStr(body map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := body[k]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func (p *avanfinityV2026) Preview(ctx context.Context, remoteCode, device string) (int, []byte, error) {
	return p.public(ctx, "preview", map[string]any{"code": strings.TrimSpace(remoteCode)}, device)
}

func (p *avanfinityV2026) Preflight(ctx context.Context, body map[string]any, device string) (int, []byte, error) {
	cred := map[string]any{"mode": "session"}
	if c, ok := body["credential"].(map[string]any); ok {
		if s, ok := c["session"].(string); ok {
			cred["session"] = s
		}
	}
	if _, ok := cred["session"]; !ok {
		if s := pickStr(body, "session"); s != "" {
			cred["session"] = s
		}
	}
	// 文档 additionalProperties=false：只发它认的字段。
	return p.public(ctx, "preflight", map[string]any{
		"redemptionToken": pickStr(body, "redemptionToken", "redemption_token", "token"),
		"credential":      cred,
	}, device)
}

func (p *avanfinityV2026) RecoverSubscription(context.Context, map[string]any, string) (int, []byte, error) {
	raw, _ := json.Marshal(map[string]any{"error": "Avanfinity 备台不支持宽限期恢复", "errorCode": "NOT_SUPPORTED"})
	return http.StatusNotImplemented, raw, nil
}

// AvanfinityRedeemClientRequestID exposes the exact wire identity so callers can
// associate their original request id with the provider's UUID. Callers with an
// empty id must retain the generated UUID and pass it back in clientRequestId.
func AvanfinityRedeemClientRequestID(reqID string) string {
	reqID = strings.TrimSpace(reqID)
	if !uuidRe.MatchString(reqID) {
		if reqID != "" {
			// 前端的 web-xxxx-时间戳 不是 UUID；按内容推导，重试仍是同一个请求。
			sum := sha1.Sum([]byte("danew-req:" + reqID))
			reqID = formatUUID(sum[:16], 5)
		} else {
			reqID = newUUID4()
		}
	}
	return reqID
}

func (p *avanfinityV2026) Redeem(ctx context.Context, body map[string]any, device string) (int, []byte, error) {
	reqID := AvanfinityRedeemClientRequestID(pickStr(body, "clientRequestId", "client_request_id"))
	return p.public(ctx, "redeem", map[string]any{
		"redemptionToken": pickStr(body, "redemptionToken", "redemption_token", "token"),
		"preflightToken":  pickStr(body, "preflightToken", "preflight_token"),
		"clientRequestId": reqID,
	}, device)
}

func (p *avanfinityV2026) Result(ctx context.Context, token, device string) (int, []byte, error) {
	return p.public(ctx, "result", map[string]any{"redemptionToken": strings.TrimSpace(token)}, device)
}

// Disable / DeleteAndRefund 文档没有对应接口，回收只在本站标记作废。
func (p *avanfinityV2026) Disable(context.Context, string) error { return ErrRefundUnsupported }

func (p *avanfinityV2026) DeleteAndRefund(context.Context, string) error {
	return ErrRefundUnsupported
}

// AvanfinityBalance 余额（字符串小数，USD）。Spendable = balance − riskLocked − retryReserved。
type AvanfinityBalance struct {
	Balance              string `json:"balance"`
	TotalBalance         string `json:"totalBalance"`
	RiskLockedBalance    string `json:"riskLockedBalance"`
	RetryReservedBalance string `json:"retryReservedBalance"`
	Spendable            string `json:"-"`
}

// AvanfinityOffer 备台 GPT CDK 在售档位与库存。
type AvanfinityOffer struct {
	Plan          string  `json:"plan"`
	Enabled       bool    `json:"enabled"`
	SalePrice     *string `json:"salePrice"`
	PricingStatus string  `json:"pricingStatus"`
	Stock         int64   `json:"stock"`
}

// AvanfinityAccount 给管理端用的只读查询（余额、库存）。
type AvanfinityAccount interface {
	Balance(ctx context.Context) (*AvanfinityBalance, error)
	Offers(ctx context.Context) ([]AvanfinityOffer, error)
}

func (p *avanfinityV2026) Balance(ctx context.Context) (*AvanfinityBalance, error) {
	var b AvanfinityBalance
	if err := p.call(ctx, http.MethodGet, "/balance", nil, nil, &b); err != nil {
		return nil, err
	}
	sp := decimalToMinor(b.Balance) - decimalToMinor(b.RiskLockedBalance) - decimalToMinor(b.RetryReservedBalance)
	if sp < 0 {
		sp = 0
	}
	b.Spendable = fmt.Sprintf("%d.%02d", sp/100, sp%100)
	return &b, nil
}

func (p *avanfinityV2026) Offers(ctx context.Context) ([]AvanfinityOffer, error) {
	var out struct {
		Offers []AvanfinityOffer `json:"offers"`
	}
	if err := p.call(ctx, http.MethodGet, "/gpt-direct/cdk-offers", nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Offers, nil
}

// addSnakeAliases 给顶层和 data 里的 camelCase 键补 snake_case 副本，
// 本站兑换页和 handler 都按 redemption_token / preflight_token 读。
func addSnakeAliases(raw []byte) []byte {
	var top map[string]any
	if json.Unmarshal(raw, &top) != nil {
		return raw
	}
	aliasMap(top)
	if d, ok := top["data"].(map[string]any); ok {
		aliasMap(d)
	}
	out, err := json.Marshal(top)
	if err != nil {
		return raw
	}
	return out
}

func aliasMap(m map[string]any) {
	for k, v := range m {
		s := camelToSnake(k)
		if s == k {
			continue
		}
		if _, exists := m[s]; !exists {
			m[s] = v
		}
	}
}

func camelToSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
