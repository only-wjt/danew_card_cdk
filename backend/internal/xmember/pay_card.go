package xmember

import (
	"context"
	"fmt"
	"strings"

	"github.com/danew/cdk-recharge-system/internal/avanfinity"
	"github.com/danew/cdk-recharge-system/internal/db"
)

// CDKPayFields 按通道上保存的付款方式，生成上游发码要用的 cardId 或 autoCard。
// X CDK 和 TG 共用这一份设置。已有卡模式会当场向卡台拉卡列表。
func CDKPayFields(ctx context.Context, client *avanfinity.Client, ch db.XChannel) (map[string]any, error) {
	mode := strings.TrimSpace(ch.PayMode)
	switch mode {
	case "":
		if ch.AutoCard {
			mode = "new"
		} else if ch.CardID > 0 {
			mode = "fixed"
		} else {
			mode = "existing"
		}
	case "new", "fixed", "existing":
	default:
		return nil, fmt.Errorf("未知的付款方式")
	}
	plan := avanfinity.PayPlan{
		Mode:      mode,
		CardID:    ch.CardID,
		Fallback:  ch.PayFallback,
		Product:   strings.TrimSpace(ch.AutoCardProduct),
		FirstName: strings.TrimSpace(ch.AutoCardFirstName),
		LastName:  strings.TrimSpace(ch.AutoCardLastName),
	}
	for _, pref := range ch.CardPrefs {
		plan.Order = append(plan.Order, avanfinity.CardPref{ID: pref.ID, Enabled: pref.Enabled})
	}
	var cards []avanfinity.Card
	if mode == "existing" {
		if client == nil {
			return nil, fmt.Errorf("读取已有卡失败")
		}
		list, err := client.ListCards(ctx)
		if err != nil {
			return nil, fmt.Errorf("读取已有卡失败：%w", err)
		}
		cards = list
	}
	return avanfinity.PayFields(plan, cards)
}
