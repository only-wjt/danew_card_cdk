package cardplatform

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// DirectPreflightResult 卡台凭据预检里本站用得到的部分。
// 报价与 preflight_token 故意不解析：本站只拿它校验 Session，不下单。
type DirectPreflightResult struct {
	Email       string `json:"email"`
	CurrentPlan string `json:"current_plan"`
}

// DirectPreflight POST /gpt-direct/preflight — 用 API Key 校验 GPT 凭据，不需要卡密。
// 只传 credential：不指定套餐与地区，卡台按默认地区预检。
func (c *Client) DirectPreflight(ctx context.Context, session string) (*DirectPreflightResult, error) {
	body := map[string]any{
		"credential": map[string]any{"mode": "session", "session": session},
	}
	data, err := c.doOpenAPI(ctx, http.MethodPost, "/gpt-direct/preflight", body, "")
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return &DirectPreflightResult{
		Email:       firstString(raw, "email", "account_email"),
		CurrentPlan: firstString(raw, "currentPlan", "current_plan"),
	}, nil
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
