package avanfinity

import (
	"fmt"
	"strings"
)

// PayPlan 是发 X / TG 码时怎么指定付款卡。
// existing：按顺序用已经开好的卡。fixed：钉死一张。new：每笔开一张新卡。
type PayPlan struct {
	Mode      string
	CardID    int64
	Order     []CardPref
	Fallback  bool
	Product   string
	FirstName string
	LastName  string
}

// CardPref 是已有卡池里的一张，以及它是否参与自动选。
type CardPref struct {
	ID      int64 `json:"id"`
	Enabled bool  `json:"enabled"`
}

// PayFields 把付款方式写成上游发码请求里的 cardId 或 autoCard。两者只留一个。
func PayFields(plan PayPlan, cards []Card) (map[string]any, error) {
	switch plan.Mode {
	case "new":
		if plan.Product == "" || plan.FirstName == "" || plan.LastName == "" {
			return nil, fmt.Errorf("自动开卡要选卡种，并填持卡人的名和姓")
		}
		return map[string]any{"autoCard": map[string]string{
			"productCode": plan.Product, "firstName": plan.FirstName, "lastName": plan.LastName,
		}}, nil
	case "fixed":
		if plan.CardID <= 0 {
			return nil, fmt.Errorf("还没指定固定卡")
		}
		return map[string]any{"cardId": plan.CardID}, nil
	case "existing", "":
		if card, ok := PickExistingCard(cards, plan.Order); ok {
			return map[string]any{"cardId": card.ID}, nil
		}
		if plan.Fallback {
			if plan.Product == "" || plan.FirstName == "" || plan.LastName == "" {
				return nil, fmt.Errorf("没有合格的已有卡，开新卡也还没填卡种和持卡人")
			}
			return map[string]any{"autoCard": map[string]string{
				"productCode": plan.Product, "firstName": plan.FirstName, "lastName": plan.LastName,
			}}, nil
		}
		return nil, fmt.Errorf("已有卡里没有能用的，这笔先停住")
	default:
		return nil, fmt.Errorf("未知的付款方式")
	}
}

// PickExistingCard 按保存的顺序挑第一张仍可用、且参与自动选的卡。
// 顺序为空时，用上游列表里第一张可用卡。
func PickExistingCard(cards []Card, order []CardPref) (Card, bool) {
	usable := make(map[int64]Card, len(cards))
	var fallback []Card
	for _, card := range cards {
		if !cardUsable(card) {
			continue
		}
		usable[card.ID] = card
		fallback = append(fallback, card)
	}
	if len(order) == 0 {
		if len(fallback) == 0 {
			return Card{}, false
		}
		return fallback[0], true
	}
	for _, pref := range order {
		if !pref.Enabled || pref.ID <= 0 {
			continue
		}
		if card, ok := usable[pref.ID]; ok {
			return card, true
		}
	}
	return Card{}, false
}

func cardUsable(card Card) bool {
	if card.ID <= 0 {
		return false
	}
	st := strings.ToLower(card.Status)
	for _, part := range []string{"frozen", "closed", "deleted", "disabled"} {
		if strings.Contains(st, part) {
			return false
		}
	}
	return true
}
