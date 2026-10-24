package cardplatform

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// 卡网 Grok「首月半价」档位：对外键 = 正价键 + "_promo"（grok_super_monthly_promo）。
//
// 卡台没有这种档位：发码按正价键发，码在本站也存正价键。半价只是兑换前的一道校验——
// Grok 按账号给首月优惠，半价码兑到没有资格的账号上，本站要按全价付款，所以只放行首月报价是半价的账号。
// 正价码不检查：兑到有优惠的账号上本站少付钱，不吃亏。
const GrokPromoSuffix = "_promo"

// grokPromoPlans 开放首月半价的裸档位：Grok 只给月付 Premium / Super / Ultra 首月五折，Plus 与年付没有。
var grokPromoPlans = map[string]bool{"premium_monthly": true, "super_monthly": true, "ultra_monthly": true}

// SplitGrokPromo 去掉半价后缀，返回正价键与是否半价键。不带后缀的键原样返回。
func SplitGrokPromo(plan string) (base string, promo bool) {
	p := strings.TrimSpace(plan)
	if b, ok := strings.CutSuffix(strings.ToLower(p), GrokPromoSuffix); ok {
		return b, true
	}
	return p, false
}

// IsGrokPromoKey 卡网的 Grok 半价键（grok_ 前缀 + _promo 后缀）。
func IsGrokPromoKey(plan string) bool {
	base, promo := SplitGrokPromo(plan)
	return promo && strings.HasPrefix(base, "grok_")
}

// HasGrokPromo 该 Grok 档位是否开放首月半价。
func HasGrokPromo(plan string) bool {
	return grokPromoPlans[GrokBarePlan(plan)]
}

// GrokPromoKey 正价 Grok 档位对应的半价键（带 grok_ 前缀）。
func GrokPromoKey(plan string) string {
	return GrokIssuePlan(plan) + GrokPromoSuffix
}

// GrokPromoQuote Grok 预检里某个档位的首月报价。
type GrokPromoQuote struct {
	AmountMinor     int64
	ListAmountMinor int64
	Currency        string
	// PromoKnown 预检带了该档位的 promotions；带了就要求 eligible，和报价交叉核对。
	PromoKnown    bool
	PromoEligible bool
}

// HalfPrice 首月实付不高于原价一半，且（若卡台给了优惠信息）账号有优惠资格。
func (q GrokPromoQuote) HalfPrice() bool {
	return q.ListAmountMinor > 0 && q.AmountMinor >= 0 && q.AmountMinor*2 <= q.ListAmountMinor &&
		(!q.PromoKnown || q.PromoEligible)
}

var ErrGrokQuoteMissing = errors.New("预检未返回该档位报价")

// ParseGrokPromoQuote 从 Grok 预检响应（卡台 envelope 的 data 或平铺）取某档位报价。
// 报价按裸键（super_monthly）给；带 grok_ 前缀、改名前的旧键（heavy_monthly）也认。
// 只解析用到的那一项：别的档位字段类型变了，不该连累这一档。
func ParseGrokPromoQuote(raw json.RawMessage, plan string) (GrokPromoQuote, error) {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return GrokPromoQuote{}, fmt.Errorf("解析预检报价: %w", err)
	}
	scope := root
	if data, ok := root["data"].(map[string]any); ok {
		if _, has := data["quotes"]; has {
			scope = data
		}
	}
	keys := grokQuoteKeys(plan)
	quotes, _ := scope["quotes"].(map[string]any)
	var q map[string]any
	for _, k := range keys {
		if v, ok := quotes[k].(map[string]any); ok {
			q = v
			break
		}
	}
	if q == nil {
		return GrokPromoQuote{}, ErrGrokQuoteMissing
	}
	out := GrokPromoQuote{
		AmountMinor:     minorOf(q, "amountMinor", "amount_minor"),
		ListAmountMinor: minorOf(q, "listAmountMinor", "list_amount_minor"),
	}
	out.Currency, _ = q["currency"].(string)
	out.Currency = strings.ToUpper(strings.TrimSpace(out.Currency))
	if out.AmountMinor < 0 || out.ListAmountMinor <= 0 {
		return GrokPromoQuote{}, ErrGrokQuoteMissing
	}
	promos, _ := scope["promotions"].(map[string]any)
	for _, k := range keys {
		if p, ok := promos[k].(map[string]any); ok {
			out.PromoKnown = true
			out.PromoEligible, _ = p["eligible"].(bool)
			break
		}
	}
	return out, nil
}

// grokQuoteKeys 报价表里可能用的键：新裸键、带前缀、旧裸键。
func grokQuoteKeys(plan string) []string {
	bare := GrokBarePlan(plan)
	keys := []string{bare, GrokIssuePlan(bare)}
	for old, cur := range grokLegacyPlans {
		if cur == bare {
			keys = append(keys, old)
		}
	}
	return keys
}

// minorOf 金额最小单位：数字或数字字符串都认，取不到返回 -1。
func minorOf(m map[string]any, keys ...string) int64 {
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			return int64(math.Round(v))
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				return int64(math.Round(f))
			}
		}
	}
	return -1
}

// 首月优惠档次（不含金额）：兑换页只告诉买家有没有优惠，不露卡台报价。
const (
	GrokOfferHalf     = "half"
	GrokOfferDiscount = "discount"
	GrokOfferNone     = "none"
)

// GrokFirstMonthOffers 预检报价里每个档位的首月优惠档次，键与 quotes 相同。
func GrokFirstMonthOffers(raw json.RawMessage) map[string]string {
	var root map[string]any
	if json.Unmarshal(raw, &root) != nil {
		return nil
	}
	scope := root
	if data, ok := root["data"].(map[string]any); ok {
		if _, has := data["quotes"]; has {
			scope = data
		}
	}
	quotes, _ := scope["quotes"].(map[string]any)
	out := map[string]string{}
	for key := range quotes {
		q, err := ParseGrokPromoQuote(raw, key)
		switch {
		case err != nil:
			continue
		case q.HalfPrice():
			out[key] = GrokOfferHalf
		case q.AmountMinor < q.ListAmountMinor:
			out[key] = GrokOfferDiscount
		default:
			out[key] = GrokOfferNone
		}
	}
	return out
}
