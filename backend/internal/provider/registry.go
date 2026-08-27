package provider

import (
	"fmt"
	"strings"

	"github.com/danew/cdk-recharge-system/internal/db"
)

// Build 按账户协议构造 adapter（测试里可替换成假实现）。
var Build = buildProvider

func buildProvider(acc db.CardPlatformAccount) (CardProvider, error) {
	switch strings.TrimSpace(acc.Protocol) {
	case ProtocolSpaceXLegacy, "":
		return NewSpaceXLegacy(acc), nil
	case ProtocolAvanfinity202608:
		// Avanfinity 域名现仍兼容 /openapi/v1 + X-API-Key（与现网 client 相同）。
		// 独立的 2026-08 REST 路径（App-Id 鉴权等）待新台凭证到位后再拆 adapter。
		return NewSpaceXLegacy(acc), nil
	default:
		return nil, fmt.Errorf("未知卡台协议 %q", acc.Protocol)
	}
}

// Registry 当前可用的卡台账户与 adapter（按 priority 升序，主台在前）。
type Registry struct {
	Accounts  []db.CardPlatformAccount
	Providers []CardProvider
}

// LoadRegistry 读账户表并构造可用 adapter。构造失败的账户跳过并记在 Skipped。
func LoadRegistry() (*Registry, []string, error) {
	accounts, err := db.ActiveDualIssueAccounts()
	if err != nil {
		return nil, nil, err
	}
	reg := &Registry{}
	var skipped []string
	for _, acc := range accounts {
		p, err := Build(acc)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %v", acc.Name, err))
			continue
		}
		reg.Accounts = append(reg.Accounts, acc)
		reg.Providers = append(reg.Providers, p)
	}
	return reg, skipped, nil
}

// ForAccount 按账户 id 取 adapter（兑换/退役按 binding 上记录的账户走）。
func ForAccount(accountID int64) (CardProvider, db.CardPlatformAccount, error) {
	all, err := db.ListCardPlatformAccounts()
	if err != nil {
		return nil, db.CardPlatformAccount{}, err
	}
	for _, acc := range all {
		if acc.ID == accountID {
			p, err := Build(acc)
			return p, acc, err
		}
	}
	return nil, db.CardPlatformAccount{}, fmt.Errorf("卡台账户 %d 不存在", accountID)
}
