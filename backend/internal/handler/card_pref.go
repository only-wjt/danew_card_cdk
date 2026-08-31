package handler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
	"github.com/danew/cdk-recharge-system/internal/db"
)

func hasEnabledSelectionRulesForAccount(accountID int64) bool {
	var rules []db.CardSelectionRule
	var err error
	if accountID > 0 {
		rules, err = db.GetCardSelectionRulesForAccount(accountID)
	} else {
		rules, err = db.GetCardSelectionRules()
	}
	if err != nil {
		return false
	}
	for _, r := range rules {
		if r.Enabled && strings.TrimSpace(r.PlanKey) != "" {
			return true
		}
	}
	return false
}

func cardProductUsable(code string, products []db.CardProductCache) bool {
	code = strings.TrimSpace(code)
	if code == "" {
		return false
	}
	if len(products) == 0 {
		return true
	}
	for _, p := range products {
		if !strings.EqualFold(p.ProductCode, code) {
			continue
		}
		return p.Enabled && strings.TrimSpace(p.SuspendedAt) == ""
	}
	return false
}

func resolveIssuerForRule(r db.CardSelectionRule, products []db.CardProductCache) string {
	if iss := cardplatform.CanonicalCardIssuer(r.Channel); iss != "" {
		return iss
	}
	for _, p := range products {
		if strings.EqualFold(p.ProductCode, strings.TrimSpace(r.PlanKey)) {
			if iss := cardplatform.CanonicalCardIssuer(p.Issuer); iss != "" {
				return iss
			}
		}
	}
	return "one"
}

func loadRulesAndProducts(accountID int64) (rules []db.CardSelectionRule, products []db.CardProductCache) {
	if accountID > 0 {
		rules, _ = db.GetCardSelectionRulesForAccount(accountID)
		products, _ = db.GetCardProductsForAccount(accountID)
		return rules, products
	}
	rules, _ = db.GetCardSelectionRules()
	products, _ = db.GetCardProducts()
	return rules, products
}

// buildSelectPriority 把本站选卡规则转成卡台 select_priority；已下线/未启用的跳过。
func buildSelectPriority(rules []db.CardSelectionRule, products []db.CardProductCache) []cardplatform.DirectCardSelectPref {
	out := make([]cardplatform.DirectCardSelectPref, 0, len(rules))
	seen := map[string]bool{}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		key := strings.TrimSpace(r.PlanKey)
		if key == "" || !cardProductUsable(key, products) {
			continue
		}
		iss := resolveIssuerForRule(r, products)
		dup := iss + "|product|" + key
		if seen[dup] {
			continue
		}
		seen[dup] = true
		out = append(out, cardplatform.DirectCardSelectPref{
			Issuer: iss, SegmentType: "product", SegmentKey: key,
		})
		if len(out) >= 12 {
			break
		}
	}
	return out
}

func firstUsableCardPref(policy SiteRedeemPolicy, rules []db.CardSelectionRule, products []db.CardProductCache) (issuer, segmentType, segmentKey string) {
	if code := strings.TrimSpace(policy.ProductCode); code != "" {
		if cardProductUsable(code, products) {
			iss := cardplatform.CanonicalCardIssuer(policy.Issuer)
			if iss == "" {
				for _, p := range products {
					if strings.EqualFold(p.ProductCode, code) {
						iss = cardplatform.CanonicalCardIssuer(p.Issuer)
						break
					}
				}
			}
			if iss == "" {
				iss = "one"
			}
			return iss, "product", code
		}
	}
	prefs := buildSelectPriority(rules, products)
	if len(prefs) == 0 {
		return "", "", ""
	}
	return prefs[0].Issuer, prefs[0].SegmentType, prefs[0].SegmentKey
}

func resolveIssueCardPrefForAccount(accountID int64, policy SiteRedeemPolicy) (issuer, segmentType, segmentKey string) {
	rules, products := loadRulesAndProducts(accountID)
	return firstUsableCardPref(policy, rules, products)
}

// resolveIssueCardPref 发码/换码用的第一条可用偏好（跳过未启动卡头）。
func resolveIssueCardPref(policy SiteRedeemPolicy) (issuer, segmentType, segmentKey string) {
	return resolveIssueCardPrefForAccount(0, policy)
}

func issuePrefFromSite() (cardplatform.IssueCardPref, bool) {
	return issuePrefFromAccount(primaryCardAccountID())
}

func isIssueTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	if ae, ok := err.(*cardplatform.APIError); ok {
		return ae.HTTPStatus == httpStatusRequestTimeout || ae.HTTPStatus == httpStatusGatewayTimeout
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "timeout") || strings.Contains(s, "deadline exceeded") || strings.Contains(s, "超时")
}

const (
	httpStatusRequestTimeout = 408
	httpStatusGatewayTimeout = 504
)

func issuePrefFromAccount(accountID int64) (cardplatform.IssueCardPref, bool) {
	issuer, segType, segKey := resolveIssueCardPrefForAccount(accountID, loadSiteRedeemPolicyForAccount(accountID))
	if segKey == "" && issuer == "" {
		return cardplatform.IssueCardPref{}, false
	}
	return cardplatform.IssueCardPref{Issuer: issuer, SegmentType: segType, SegmentKey: segKey}, true
}

func injectRedeemCardPolicy(body map[string]any, accountID int64) {
	if body == nil || db.DB == nil {
		return
	}
	policy := loadSiteRedeemPolicyForAccount(accountID)
	hasRules := hasEnabledSelectionRulesForAccount(accountID)
	if policy.Enabled {
		if _, exists := body["no_auto_card_switch"]; !exists {
			body["no_auto_card_switch"] = policy.NoAutoCardSwitch
		}
	}
	if _, exists := body["strict_card_preference"]; !exists && (policy.Enabled || hasRules) {
		if policy.Enabled {
			body["strict_card_preference"] = policy.StrictCardPreference
		} else {
			body["strict_card_preference"] = true
		}
	}
}

// SyncOwnerDirectCardRules 把本站选卡优先级推到指定卡台账户（或兼容旧全局配置）。
func SyncOwnerDirectCardRules(ctx context.Context, accountID int64) error {
	if accountID > 0 {
		acc, err := db.GetCardPlatformAccount(accountID)
		if err != nil {
			return err
		}
		if strings.TrimSpace(acc.CredSecret) == "" {
			return fmt.Errorf("该卡台账户未配置 API Key，规则仅保存在本站")
		}
	} else if strings.TrimSpace(cardplatform.LoadConfig().APIKey) == "" {
		return fmt.Errorf("未配置卡台 API Key，规则仅保存在本站")
	}
	cli := cardClientOrSettings(accountID)
	rules, products := loadRulesAndProducts(accountID)
	prefs := buildSelectPriority(rules, products)
	policy := loadSiteRedeemPolicyForAccount(accountID)
	existing, err := cli.GetCardRules(ctx, "")
	if err != nil {
		return err
	}
	byProd := map[string]cardplatform.DirectCardRule{}
	for _, r := range existing {
		byProd[strings.ToLower(strings.TrimSpace(r.Product))] = r
	}
	for _, prod := range []string{"gpt", "claude", "grok"} {
		cur, ok := byProd[prod]
		if !ok {
			cur = cardplatform.DirectCardRule{
				Product:          prod,
				CountFailures:    true,
				LightMaxUses:     5,
				Pro20MaxUses:     3,
				AutoSwitchOnFail: true,
				MaxAutoSwitches:  2,
				SelectMode:       "default",
			}
		}
		cur.Product = prod
		cur.SelectPriority = prefs
		cur.StrictSelect = len(prefs) > 0
		if policy.Enabled {
			cur.AutoSwitchOnFail = !policy.NoAutoCardSwitch
		}
		if _, err := cli.PutCardRule(ctx, cur); err != nil {
			return err
		}
		log.Printf("[card-rules-sync] account=%d product=%s priority=%d strict=%v", accountID, prod, len(prefs), cur.StrictSelect)
	}
	return nil
}
