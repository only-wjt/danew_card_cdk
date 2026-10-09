package xmember

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
		Mode:       mode,
		CardID:     ch.CardID,
		Fallback:   ch.PayFallback,
		Product:    strings.TrimSpace(ch.AutoCardProduct),
		FirstName:  strings.TrimSpace(ch.AutoCardFirstName),
		LastName:   strings.TrimSpace(ch.AutoCardLastName),
		ExtraTries: ch.PayExtraTries,
		MinBalance: strings.TrimSpace(ch.PayMinBalance),
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

// CDKCardCandidates 是发码时可以依次尝试的卡。只有「按顺序用已有卡」才多于一张。
func CDKCardCandidates(ctx context.Context, client *avanfinity.Client, ch db.XChannel) ([]int64, error) {
	mode := strings.TrimSpace(ch.PayMode)
	if mode == "" {
		if ch.AutoCard {
			mode = "new"
		} else if ch.CardID > 0 {
			mode = "fixed"
		} else {
			mode = "existing"
		}
	}
	if mode != "existing" || ch.PayExtraTries <= 0 || client == nil {
		return nil, nil
	}
	list, err := client.ListCards(ctx)
	if err != nil {
		return nil, err
	}
	plan := avanfinity.PayPlan{
		Mode: "existing", ExtraTries: ch.PayExtraTries, MinBalance: strings.TrimSpace(ch.PayMinBalance),
	}
	for _, pref := range ch.CardPrefs {
		plan.Order = append(plan.Order, avanfinity.CardPref{ID: pref.ID, Enabled: pref.Enabled})
	}
	ids := avanfinity.CandidateCardIDs(plan, list)
	if len(ids) <= 1 {
		return nil, nil
	}
	return ids, nil
}

// CDKTryIDs 是这一批发码要依次尝试的卡。没有可换的卡时返回 [0]，调用方不要改请求体。
func CDKTryIDs(ctx context.Context, client *avanfinity.Client, ch db.XChannel) ([]int64, error) {
	ids, err := CDKCardCandidates(ctx, client, ch)
	if err != nil {
		return nil, fmt.Errorf("读取已有卡失败：%w", err)
	}
	if len(ids) == 0 {
		return []int64{0}, nil
	}
	return ids, nil
}

// CardRefused 表示上游明确拒绝了这张卡或这次参数，可以换下一张再发。
// 超时、5xx、网络错误不能换：那一笔可能已经收下，只能用原幂等键重放。
func CardRefused(err error) bool {
	var api *avanfinity.APIError
	if !errors.As(err, &api) {
		return false
	}
	return api.Status == http.StatusBadRequest || api.Status == http.StatusNotFound || api.Status == http.StatusUnprocessableEntity
}
