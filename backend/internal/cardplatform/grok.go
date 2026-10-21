package cardplatform

import (
	"regexp"
	"strings"
)

// Grok 档位：卡台注册表里是裸键（premium_monthly / plus_yearly …），ACC 定价表里带 grok_ 前缀。
//
// 2026-10-19 卡台改名对齐 xAI 现行四档 Plus / Premium / Super / Ultra：
// lite→plus($8)、monthly/yearly→premium($30)、plus→super($100)、heavy→ultra($200)。
// ★裸 premium_monthly 同时是 X Premium 的键★：没有产品信息时 X 优先（issueProduct 先判 X），
// Grok 发码一律带 grok_ 前缀（GrokIssuePlan）。旧键 lite/heavy/monthly/yearly 仍认，按新键处理。
// 带 grok_ 前缀时认全部四档；裸键不认 premium_*（归 X）。
var grokPlanRe = regexp.MustCompile(`^(?:grok_(?:(?:plus|premium|super|ultra|lite|heavy)_)?|(?:(?:plus|super|ultra|lite|heavy)_)?)(?:monthly|yearly)$`)

func IsGrokPlan(plan string) bool {
	return grokPlanRe.MatchString(strings.ToLower(strings.TrimSpace(plan)))
}

// grokLegacyPlans 改名前的旧裸键 → 新裸键。★不含 plus_*★：它现在就是新键（$8 档）。
var grokLegacyPlans = map[string]string{
	"lite_monthly": "plus_monthly", "lite_yearly": "plus_yearly",
	"monthly": "premium_monthly", "yearly": "premium_yearly",
	"heavy_monthly": "ultra_monthly", "heavy_yearly": "ultra_yearly",
}

// GrokBarePlan 去掉 grok_ 前缀的裸档位（本站界面与码上都用裸键，与 X 一致），旧键换成新键。
func GrokBarePlan(plan string) string {
	p := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(plan)), "grok_")
	if n, ok := grokLegacyPlans[p]; ok {
		return n
	}
	return p
}

// GrokIssuePlan 发给卡台的发码档位：显式带 grok_ 前缀。
// 卡台也认裸键，但裸键要靠「遍历各产品档位表」反查产品，将来哪个产品也有 monthly 就会认错；
// 带前缀则直接定位到 Grok。卡台落库时会去掉前缀。
func GrokIssuePlan(plan string) string {
	return "grok_" + GrokBarePlan(plan)
}

// IsGrokCredential Grok 登录态（grok.com 的 sso cookie，含 sso= / sso-rw=）。
// 它是可复用的登录凭据，不是 ChatGPT 账单会话，不能绑到卡密供账单页查询。
func IsGrokCredential(raw string) bool {
	s := strings.TrimSpace(raw)
	if s == "" || strings.HasPrefix(s, "{") {
		return false
	}
	return strings.HasPrefix(s, "sso=") || strings.HasPrefix(s, "sso-rw=") ||
		strings.Contains(s, "; sso=") || strings.Contains(s, ";sso=") ||
		strings.Contains(s, "; sso-rw=") || strings.Contains(s, ";sso-rw=")
}

var grokTierLabels = map[string]string{"plus": "Plus", "premium": "Premium", "super": "Super", "ultra": "Ultra"}

// GrokPlanLabel 带 grok_ 前缀的档位可读名（如 "Grok Premium 月付"）；不是 Grok 档位返回空串。
// 裸 premium_* 归 X，这里不认。
func GrokPlanLabel(plan string) string {
	p := strings.ToLower(strings.TrimSpace(plan))
	if !strings.HasPrefix(p, "grok_") || !IsGrokPlan(p) {
		return ""
	}
	bare := GrokBarePlan(p)
	i := strings.LastIndex(bare, "_")
	if i < 0 {
		return ""
	}
	tier, ok := grokTierLabels[bare[:i]]
	if !ok {
		return ""
	}
	period := "月付"
	if bare[i+1:] == "yearly" {
		period = "年付"
	}
	return "Grok " + tier + " " + period
}

// NormalizeListedPlan 卡台列表/同步回来的档位是裸键且不带产品信息。能确定是 Grok 的裸键
// （plus_/super_/ultra_ 及旧 lite_/heavy_/monthly/yearly，X 没有这些键）补成 grok_ 前缀，
// 与本站发码时落库的写法一致。裸 premium_* 与 X 同名，判不了，原样返回。
func NormalizeListedPlan(plan string) string {
	p := strings.TrimSpace(plan)
	lower := strings.ToLower(p)
	if lower == "" || strings.HasPrefix(lower, "grok_") || IsXPremiumPlan(lower) || !IsGrokPlan(lower) {
		return p
	}
	return GrokIssuePlan(lower)
}
