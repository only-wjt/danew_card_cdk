package avanfinity

import (
	"fmt"
	"strconv"
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
	// ExtraTries 是第一张被上游明确拒绝后，还能再试后面几张。0 表示不换。最多 3。
	// 只用于发码。兑换开始后这张码钉死在发出去的卡上。
	ExtraTries int
	// MinBalance 是美元十进制。余额读得到且低于它的卡不参与。空或 0 表示不看余额。
	MinBalance string
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
		if ids := CandidateCardIDs(plan, cards); len(ids) > 0 {
			return map[string]any{"cardId": ids[0]}, nil
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
	ids := CandidateCardIDs(PayPlan{Mode: "existing", Order: order, ExtraTries: 0}, cards)
	if len(ids) == 0 {
		return Card{}, false
	}
	for _, card := range cards {
		if card.ID == ids[0] {
			return card, true
		}
	}
	return Card{ID: ids[0]}, true
}

// CandidateCardIDs 按顺序给出发码可以依次尝试的卡。
// 第一张是下一笔，后面最多 ExtraTries 张，给「这张被拒再试后面的」用。
func CandidateCardIDs(plan PayPlan, cards []Card) []int64 {
	if plan.Mode == "fixed" {
		if plan.CardID > 0 {
			return []int64{plan.CardID}
		}
		return nil
	}
	if plan.Mode != "" && plan.Mode != "existing" {
		return nil
	}
	extra := plan.ExtraTries
	if extra < 0 {
		extra = 0
	}
	if extra > 3 {
		extra = 3
	}
	min := parseUSD(plan.MinBalance)
	usable := make(map[int64]Card, len(cards))
	var fallback []Card
	for _, card := range cards {
		if !cardMeets(card, min) {
			continue
		}
		usable[card.ID] = card
		fallback = append(fallback, card)
	}
	var ordered []Card
	if len(plan.Order) == 0 {
		ordered = fallback
	} else {
		for _, pref := range plan.Order {
			if !pref.Enabled || pref.ID <= 0 {
				continue
			}
			if card, ok := usable[pref.ID]; ok {
				ordered = append(ordered, card)
			}
		}
	}
	limit := 1 + extra
	if len(ordered) > limit {
		ordered = ordered[:limit]
	}
	ids := make([]int64, 0, len(ordered))
	for _, card := range ordered {
		ids = append(ids, card.ID)
	}
	return ids
}

func parseUSD(raw string) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func cardMeets(card Card, min float64) bool {
	if !cardUsable(card) {
		return false
	}
	if min <= 0 {
		return true
	}
	raw := strings.TrimSpace(card.Balance)
	if raw == "" {
		return true
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return true
	}
	return n+1e-9 >= min
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
