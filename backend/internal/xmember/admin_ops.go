package xmember

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/danew/cdk-recharge-system/internal/avanfinity"
	"github.com/danew/cdk-recharge-system/internal/db"
)

// ChannelStrip 是后台顶部一条通道的余额和负债。
type ChannelStrip struct {
	Channel         string `json:"channel"`
	Enabled         bool   `json:"enabled"`
	AccountID       int64  `json:"account_id"`
	AccountName     string `json:"account_name"`
	WalletUSD       string `json:"wallet_usd"`
	LiabilityUSD    string `json:"liability_usd"`
	Unused          int    `json:"unused"`
	CardBalance     string `json:"card_balance"`
	CardLabel       string `json:"card_label"`
	PaymentsEnabled *bool  `json:"payments_enabled,omitempty"`
	Alert           string `json:"alert"`
}

// ChannelStrips 读取两个通道当前卡台的余额。上游失败只写在 alert 里，不让整页打不开。
func ChannelStrips(ctx context.Context) []ChannelStrip {
	channels, err := db.ListXChannels()
	if err != nil {
		return nil
	}
	out := make([]ChannelStrip, 0, len(channels))
	for _, ch := range channels {
		strip := ChannelStrip{Channel: ch.Channel, Enabled: ch.Enabled, AccountID: ch.AccountID}
		unused, liability, _ := db.CountUnusedXCodes(ch.Channel)
		strip.Unused = unused
		strip.LiabilityUSD = E4ToUSD(liability)
		if ch.AccountID <= 0 {
			strip.Alert = "还没绑定卡台"
			out = append(out, strip)
			continue
		}
		acc, err := db.GetCardPlatformAccount(ch.AccountID)
		if err != nil {
			strip.Alert = "卡台不存在"
			out = append(out, strip)
			continue
		}
		strip.AccountName = acc.Name
		if !strings.EqualFold(acc.Status, "active") {
			strip.Alert = "卡台已停用"
		}
		cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		client := clientFor(acc)
		if bal, err := client.GetBalance(cctx); err != nil {
			if strip.Alert == "" {
				strip.Alert = "读不到钱包余额"
			}
		} else if bal != nil {
			strip.WalletUSD = bal.Balance
			if belowUSD(bal.Balance, settingUSD("x_alert_wallet_usd")) && strip.Alert == "" {
				strip.Alert = "钱包余额低于告警线"
			}
		}
		if plans, err := client.GetXPlans(cctx); err == nil && plans != nil {
			on := plans.PaymentsEnabled
			strip.PaymentsEnabled = &on
			if !on && strip.Alert == "" {
				strip.Alert = "上游已关闭付款"
			}
		}
		if cards, err := client.ListCards(cctx); err == nil {
			for _, card := range cards {
				if ch.CardID > 0 && card.ID == ch.CardID {
					strip.CardBalance = card.Balance
					strip.CardLabel = card.CardNumberMasked
					if belowUSD(card.Balance, settingUSD("x_alert_card_usd")) && strip.Alert == "" {
						strip.Alert = "付款卡余额低于告警线"
					}
				}
			}
		}
		cancel()
		out = append(out, strip)
	}
	return out
}

func settingUSD(key string) string {
	v, _ := db.GetSetting(key)
	return strings.TrimSpace(v)
}

func belowUSD(have, floor string) bool {
	if floor == "" || have == "" {
		return false
	}
	h, ok1 := USDToE4(have)
	f, ok2 := USDToE4(floor)
	return ok1 && ok2 && h < f
}

// TestQuote 试报价：直充 prepare 后立刻 cancel；CDK 发 1 张、预检、再撤销。不留给客户。
func TestQuote(ctx context.Context, channel, plan, handle string) (db.XQuoteSample, error) {
	name := NormalizeHandle(handle)
	if !ValidHandle(name) {
		return db.XQuoteSample{}, fmt.Errorf("用户名只能是 1–15 位字母、数字或下划线")
	}
	ch, acc, err := loadChannel(channel)
	if err != nil {
		return db.XQuoteSample{}, err
	}
	limits, err := db.ListXPlanLimits(channel)
	if err != nil {
		return db.XQuoteSample{}, err
	}
	var limit db.XPlanLimit
	for _, row := range limits {
		if row.Plan == plan {
			limit = row
		}
	}
	if limit.Plan == "" {
		return db.XQuoteSample{}, fmt.Errorf("未知套餐")
	}
	client := clientFor(acc)
	if channel == db.XChannelDirect {
		if ch.CardID <= 0 {
			return db.XQuoteSample{}, fmt.Errorf("先在卡台里选一张付款卡")
		}
		order, err := client.PrepareDirect(ctx, NewUUID(), map[string]any{
			"recipient": name, "plan": plan, "cardId": ch.CardID, "clientRequestId": NewUUID(),
		})
		noteCall(acc.ID, "POST", "/x-direct/orders/prepare", err)
		if err != nil {
			return db.XQuoteSample{}, err
		}
		sample := sampleFromDirect(channel, plan, "probe", order)
		if order != nil && order.ID != "" && !order.PaymentAttempted {
			err = client.CancelDirect(ctx, order.ID, NewUUID())
			noteCall(acc.ID, "POST", "/x-direct/orders/cancel", err)
			if err != nil {
				return sample, fmt.Errorf("报价已记下，但取消失败：%w", err)
			}
		}
		_ = db.InsertQuoteSample(sample.Channel, sample.Plan, sample.Currency, sample.Source, sample.AmountMinor, sample.EstimatedUSDE4, sample.ServiceFeeE4, 0)
		return sample, nil
	}
	if err := limitsReady(channel, limit); err != nil {
		return db.XQuoteSample{}, err
	}
	wallet, _ := USDToE4(limit.MaxWalletDebitUSD)
	funding, _ := USDToE4(limit.FundingAmountUSD)
	body := map[string]any{
		"plan": plan, "quantity": 1,
		"maxWalletDebitUsd": E4ToUSD(wallet), "maxOfficialAmountMinor": limit.MaxOfficialAmountMinor,
		"currency": strings.ToLower(limit.Currency), "fundingAmountUsd": E4ToUSD(funding),
	}
	if ch.AutoCard {
		body["autoCard"] = map[string]string{
			"productCode": ch.AutoCardProduct, "firstName": ch.AutoCardFirstName, "lastName": ch.AutoCardLastName,
		}
	} else {
		body["cardId"] = ch.CardID
	}
	gen, err := client.GenerateCDKs(ctx, NewUUID(), body)
	noteCall(acc.ID, "POST", "/x-direct/cdks/generate", err)
	if err != nil {
		return db.XQuoteSample{}, err
	}
	if gen == nil || len(gen.List) == 0 {
		return db.XQuoteSample{}, fmt.Errorf("上游没有返回测试码")
	}
	item := gen.List[0]
	defer func() {
		if item.ID != "" {
			err := client.RevokeCDK(ctx, item.ID, NewUUID())
			noteCall(acc.ID, "POST", "/x-direct/cdks/revoke", err)
		}
	}()
	if item.Code == "" {
		item.Code, err = client.ExportCDK(ctx, item.ID)
		noteCall(acc.ID, "GET", "/x-direct/cdks/export", err)
		if err != nil {
			return db.XQuoteSample{}, err
		}
	}
	pub, err := client.PreflightCDK(ctx, item.Code, NewDeviceToken(), name, NewUUID())
	noteCall(acc.ID, "POST", "/public/x-cdk/preflight", err)
	if err != nil {
		return db.XQuoteSample{}, err
	}
	sample := db.XQuoteSample{
		Channel: channel, Plan: plan, Currency: strings.ToLower(pub.Currency),
		AmountMinor: pub.AmountMinor, Source: "probe",
	}
	_ = db.InsertQuoteSample(sample.Channel, sample.Plan, sample.Currency, sample.Source, sample.AmountMinor, 0, 0, 0)
	return sample, nil
}

func sampleFromDirect(channel, plan, source string, order *avanfinity.DirectOrder) db.XQuoteSample {
	sample := db.XQuoteSample{Channel: channel, Plan: plan, Source: source}
	if order == nil {
		return sample
	}
	sample.Currency = strings.ToLower(order.Currency)
	sample.AmountMinor = order.AmountMinor
	if v, ok := USDToE4(order.EstimatedUSD); ok {
		sample.EstimatedUSDE4 = v
	}
	if v, ok := USDToE4(order.ServiceFee); ok {
		sample.ServiceFeeE4 = v
	}
	return sample
}
