package cardplatform

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// CanonicalCardIssuer 把 CDK 历史写法（ch1/ch3/ch4）归一成卡台 issuer。
// 认不出返回空，调用方再从产品缓存补。
func CanonicalCardIssuer(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "one", "1", "ch1", "channel1", "vmcard", "vmcardio":
		return "one"
	case "two", "2", "ch2", "channel2":
		return "two"
	case "three", "3", "ch3", "channel3", "yika", "yikahk":
		return "three"
	case "four", "4", "ch4", "channel4", "photon", "photonpay":
		return "four"
	default:
		return ""
	}
}

// DirectCardSelectPref 卡台 PUT /gpt-direct/card-rules 的一条优先级。
type DirectCardSelectPref struct {
	Issuer      string `json:"issuer"`
	SegmentType string `json:"segment_type,omitempty"`
	SegmentKey  string `json:"segment_key"`
}

// DirectCardRule 卡台用卡规则（gpt / claude / grok 各一份）。
type DirectCardRule struct {
	Product          string                 `json:"product"`
	CountFailures    bool                   `json:"count_failures"`
	LightMaxUses     int                    `json:"light_max_uses"`
	Pro20MaxUses     int                    `json:"pro20_max_uses"`
	AutoSwitchOnFail bool                   `json:"auto_switch_on_fail"`
	MaxAutoSwitches  int                    `json:"max_auto_switches"`
	SelectMode       string                 `json:"select_mode"`
	SelectPriority   []DirectCardSelectPref `json:"select_priority"`
	StrictSelect     bool                   `json:"strict_select"`
	IsDefault        bool                   `json:"is_default,omitempty"`
}

// DirectCardProduct 卡台 GET /gpt-direct/card-products 条目（含未启动）。
type DirectCardProduct struct {
	ProductCode      string `json:"product_code"`
	Issuer           string `json:"issuer"`
	BIN              string `json:"bin"`
	Label            string `json:"label"`
	Enabled          bool   `json:"enabled"`
	Suspended        bool   `json:"suspended"`
	SuspendReason    string `json:"suspend_reason,omitempty"`
	ChannelOpen      bool   `json:"channel_open"`
	AutoOpenAllowed  bool   `json:"auto_open_allowed"`
	Usable           bool   `json:"usable"`
}

// GetDirectCardProducts GET /gpt-direct/card-products — 全量卡头及是否启动。
func (c *Client) GetDirectCardProducts(ctx context.Context) ([]DirectCardProduct, error) {
	data, err := c.doOpenAPI(ctx, http.MethodGet, "/gpt-direct/card-products", nil, "")
	if err != nil {
		return nil, err
	}
	var items []DirectCardProduct
	if err := json.Unmarshal(data, &items); err == nil {
		return items, nil
	}
	var wrapped struct {
		List []DirectCardProduct `json:"list"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.List, nil
}

// GetCardRules GET /gpt-direct/card-rules — product 空则返回三份。
func (c *Client) GetCardRules(ctx context.Context, product string) ([]DirectCardRule, error) {
	path := "/gpt-direct/card-rules"
	if p := strings.TrimSpace(product); p != "" {
		path += "?product=" + p
	}
	data, err := c.doOpenAPI(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return nil, err
	}
	var one DirectCardRule
	if err := json.Unmarshal(data, &one); err == nil && one.Product != "" {
		return []DirectCardRule{one}, nil
	}
	var wrapped struct {
		List []DirectCardRule `json:"list"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.List, nil
}

// PutCardRule PUT /gpt-direct/card-rules
func (c *Client) PutCardRule(ctx context.Context, rule DirectCardRule) (DirectCardRule, error) {
	var out DirectCardRule
	data, err := c.doOpenAPI(ctx, http.MethodPut, "/gpt-direct/card-rules", rule, "")
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	return out, nil
}
