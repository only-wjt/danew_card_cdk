package handler

import (
	"strings"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/danew/cdk-recharge-system/internal/provider"
)

// 本站统一发码开关。关闭时完全走老路径（直接卖卡台原生码），
// 开启后新发的码都是 DN- 本站码并双绑 A/B。老码不受开关影响，永远按原台兑换。
const (
	settingDualBindEnabled  = "site_dual_bind_enabled"
	settingDualBindDegraded = "site_dual_bind_allow_single"
)

func settingTruthy(key string) bool {
	v, _ := db.GetSetting(key)
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// siteDualBindEnabled 是否用本站统一发码。
func siteDualBindEnabled() bool {
	return settingTruthy(settingDualBindEnabled)
}

// allowDegradedSingleBind 只有一台成功时是否仍然出货。
// 默认关：单绑的码失去了切台能力，卖出去等于埋雷，宁可发码失败。
func allowDegradedSingleBind() bool {
	return settingTruthy(settingDualBindDegraded)
}

// cardIssuingReady 非本地库存档位是否具备自动发码能力。
func cardIssuingReady() bool {
	if siteDualBindEnabled() {
		reg, _, err := provider.LoadRegistry()
		if err != nil {
			return false
		}
		if allowDegradedSingleBind() {
			return len(reg.Providers) > 0
		}
		return len(reg.Providers) >= 2
	}
	return cardplatform.LoadConfig().APIKey != ""
}

func legacyCardClient() *cardplatform.Client {
	acc, err := db.LegacyCardPlatformAccount()
	if err != nil || strings.TrimSpace(acc.CredSecret) == "" {
		return cardplatform.NewFromSettings()
	}
	base := strings.TrimRight(strings.TrimSpace(acc.SiteBase), "/")
	base = strings.TrimSuffix(base, "/openapi/v1")
	base = strings.TrimSuffix(base, "/openapi")
	return cardplatform.New(cardplatform.Config{SiteBase: base, APIKey: strings.TrimSpace(acc.CredSecret)})
}

func legacyCardClientForCode(code string) *cardplatform.Client {
	acc, err := db.CardPlatformAccountForLegacyCode(code)
	if err != nil || strings.TrimSpace(acc.CredSecret) == "" {
		return legacyCardClient()
	}
	base := strings.TrimRight(strings.TrimSpace(acc.SiteBase), "/")
	base = strings.TrimSuffix(base, "/openapi/v1")
	base = strings.TrimSuffix(base, "/openapi")
	return cardplatform.New(cardplatform.Config{SiteBase: base, APIKey: strings.TrimSpace(acc.CredSecret)})
}

func cardClientForAccount(accountID int64) (*cardplatform.Client, error) {
	acc, err := db.GetCardPlatformAccount(accountID)
	if err != nil {
		return nil, err
	}
	return cardplatform.NewFromAccount(acc), nil
}

func cardClientOrSettings(accountID int64) *cardplatform.Client {
	if accountID > 0 {
		if cli, err := cardClientForAccount(accountID); err == nil && cli != nil {
			return cli
		}
	}
	return cardplatform.NewFromSettings()
}

func primaryCardAccountID() int64 {
	acc, err := db.PrimaryCardPlatformAccount()
	if err != nil {
		return 0
	}
	return acc.ID
}
