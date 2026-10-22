package cardplatform

import (
	"errors"
	"strings"
)

// 卡网（cardnet）对外的档位键：GPT 原样；Grok 一律带 grok_ 前缀；X 不开放给卡网。
//
// 裸 premium_monthly 同时是 X 和 Grok 的键，卡网侧没有产品信息，所以 Grok 必须带前缀，
// 与本站库里 Grok 码的写法、发给卡台的写法（GrokIssuePlan）一致。
const (
	CardnetProductGPT  = "gpt"
	CardnetProductGrok = "grok"
	// CardnetGrokCountry Grok 是美元固定价，卡台兑换时一律按 US 收单。
	CardnetGrokCountry = "US"
)

// CardnetCanonicalPlan 卡网档位键规范化：发码、客户端授权、目录过滤都先过这里。
// Grok 旧键（grok_lite_monthly 等）换成新键；X 键、裸 Grok 键、未知的 grok_ 键一律报错。
func CardnetCanonicalPlan(raw string) (plan, product string, err error) {
	p := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case p == "":
		return "", "", errors.New("plan 必填")
	case strings.HasPrefix(p, "grok_"):
		if !IsGrokPlan(p) {
			return "", "", errors.New("未知的 Grok 档位 " + p)
		}
		return GrokIssuePlan(p), CardnetProductGrok, nil
	case IsXPremiumPlan(p):
		msg := "X 订阅不支持卡网发码：" + p
		if !strings.HasPrefix(p, "x_") && IsGrokPlan("grok_"+p) {
			msg += "（Grok 请用 grok_" + p + "）"
		}
		return "", "", errors.New(msg)
	case IsGrokPlan(p):
		return "", "", errors.New("Grok 档位须带 grok_ 前缀：grok_" + GrokBarePlan(p))
	}
	return p, CardnetProductGPT, nil
}

// CardnetPlanProduct 已规范化的键属于哪个产品（库里存的就是规范化后的键）。
func CardnetPlanProduct(plan string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(plan)), "grok_") {
		return CardnetProductGrok
	}
	return CardnetProductGPT
}
