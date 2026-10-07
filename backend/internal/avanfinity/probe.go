package avanfinity

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ProbeStep 是测试连接的一步。State 取 ok / fail / skip。
type ProbeStep struct {
	Key    string `json:"key"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}

// ProbeResult 按顺序检查凭证、X 权限、卡片、写接口白名单。某一步失败后不再继续。
type ProbeResult struct {
	OK              bool        `json:"ok"`
	Steps           []ProbeStep `json:"steps"`
	Balance         string      `json:"balance,omitempty"`
	PaymentsEnabled bool        `json:"payments_enabled"`
	PlanCount       int         `json:"plan_count"`
	Cards           []Card      `json:"cards,omitempty"`
}

func Probe(ctx context.Context, c *Client) ProbeResult {
	out := ProbeResult{}
	bal, err := c.GetBalance(ctx)
	if err != nil {
		out.Steps = append(out.Steps, failStep("cred", "地址和凭证", explainErr(err, "凭证")))
		return out
	}
	out.Balance = bal.Balance
	out.Steps = append(out.Steps, ProbeStep{Key: "cred", Title: "地址和凭证", State: "ok", Detail: "通过 · 钱包余额 $" + bal.Balance})

	plans, err := c.GetXPlans(ctx)
	if err != nil {
		out.Steps = append(out.Steps, failStep("scope", "X 权限", explainScope(err)))
		return out
	}
	out.PlanCount = len(plans.Plans)
	out.PaymentsEnabled = plans.PaymentsEnabled
	pay := "已开"
	if !plans.PaymentsEnabled {
		pay = "未开"
	}
	out.Steps = append(out.Steps, ProbeStep{
		Key: "scope", Title: "X 权限", State: "ok",
		Detail: fmt.Sprintf("通过 · %d 个套餐，X 付款开关 %s", len(plans.Plans), pay),
	})

	cards, err := c.ListCards(ctx)
	if err != nil {
		out.Steps = append(out.Steps, failStep("cards", "可用卡片", explainErr(err, "读卡")))
		return out
	}
	out.Cards = cards
	out.Steps = append(out.Steps, ProbeStep{
		Key: "cards", Title: "可用卡片", State: "ok",
		Detail: fmt.Sprintf("通过 · %d 张卡", len(cards)),
	})

	card, ok := UsableCard(cards)
	if !ok {
		out.Steps = append(out.Steps, ProbeStep{
			Key: "ip", Title: "写接口白名单", State: "skip",
			Detail: "没有可用卡，跳过。自动开卡会真的开一张卡，所以不在测试里做。先开一张卡再测。",
		})
		out.OK = true
		return out
	}
	detail, err := c.ProbeWriteAllowlist(ctx, card.ID)
	if err != nil {
		var api *APIError
		if errors.As(err, &api) && businessReject(api.Status) {
			out.Steps = append(out.Steps, ProbeStep{Key: "ip", Title: "写接口白名单", State: "ok", Detail: explainWrite(err)})
			out.OK = true
			return out
		}
		out.Steps = append(out.Steps, failStep("ip", "写接口白名单", explainWrite(err)))
		return out
	}
	out.Steps = append(out.Steps, ProbeStep{Key: "ip", Title: "写接口白名单", State: "ok", Detail: detail})
	out.OK = true
	return out
}

// businessReject：409/422 说明请求已过鉴权和 IP 白名单，是被业务校验拒绝的。
// 探测码的钱包授权只有 0.01 美元，Avan 一般会以 409「超过钱包授权上限」拒绝。
func businessReject(status int) bool {
	return status == 409 || status == 422
}

func failStep(key, title, detail string) ProbeStep {
	return ProbeStep{Key: key, Title: title, State: "fail", Detail: detail}
}

func explainErr(err error, what string) string {
	var api *APIError
	if errors.As(err, &api) {
		if api.Status == 401 {
			return "401：App ID 或 App Secret 不对"
		}
		if api.Message != "" {
			return fmt.Sprintf("%d：%s", api.Status, api.Message)
		}
		return fmt.Sprintf("%s失败（HTTP %d）", what, api.Status)
	}
	return what + "失败：" + err.Error()
}

func explainScope(err error) string {
	var api *APIError
	if errors.As(err, &api) && api.Status == 403 {
		return "403：这个 App 没有开 X 权限。只读接口不看白名单，所以这不是 IP 问题。"
	}
	return explainErr(err, "查套餐")
}

func explainWrite(err error) string {
	var api *APIError
	if errors.As(err, &api) && api.Status == 403 {
		return "403：写接口被拒绝。前面的只读接口已经通过，通常是出口 IP 不在这个 App 的白名单。"
	}
	if errors.As(err, &api) && businessReject(api.Status) {
		return "白名单已通过，但测试发码被业务规则拒绝（没有留下可用卡密）：" + api.Message
	}
	msg := err.Error()
	if strings.Contains(msg, "撤销失败") {
		return msg
	}
	return explainErr(err, "测试发码")
}
