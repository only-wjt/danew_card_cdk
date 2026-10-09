package tgmember

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/danew/cdk-recharge-system/internal/avanfinity"
	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/danew/cdk-recharge-system/internal/xmember"
	"github.com/danew/cdk-recharge-system/internal/xmember/xlogic"
)

const channel = db.TGChannel

type PublicState struct {
	Code      string `json:"code"`
	Plan      string `json:"plan"`
	PlanLabel string `json:"plan_label"`
	Status    string `json:"status"`
	Step      string `json:"step"`
	Headline  string `json:"headline"`
	Detail    string `json:"detail"`
	Recipient string `json:"recipient"`
	Reusable  bool   `json:"reusable"`
	Queued    bool   `json:"queued"`
}

func PlanLabel(plan string) string {
	switch plan {
	case "premium_3m":
		return "Premium · 3 个月"
	case "premium_6m":
		return "Premium · 6 个月"
	case "premium_12m":
		return "Premium · 12 个月"
	default:
		return plan
	}
}

func NormalizeHandle(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	s = strings.TrimPrefix(s, "www.")
	lower := strings.ToLower(s)
	for _, host := range []string{"t.me/", "telegram.me/"} {
		if strings.HasPrefix(lower, host) {
			s = s[len(host):]
			break
		}
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimPrefix(strings.TrimSpace(s), "@")
}

func ValidHandle(raw string) bool {
	s := NormalizeHandle(raw)
	if len(s) < 1 || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func newToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func newSiteCode() string {
	h := strings.ToUpper(newToken(12))
	return "DNT-" + h[0:8] + "-" + h[8:16] + "-" + h[16:24]
}

func now() string { return time.Now().UTC().Format("2006-01-02 15:04:05") }

var redLocks sync.Map

func tryRed(id int64) bool {
	_, loaded := redLocks.LoadOrStore(id, true)
	return !loaded
}
func unlockRed(id int64) { redLocks.Delete(id) }

func noteCall(accountID int64, method, path string, err error) {
	status := 200
	detail := ""
	var api *avanfinity.APIError
	if errors.As(err, &api) {
		status = api.Status
		detail = api.ErrorCode
		if detail == "" {
			detail = api.Message
		}
	} else if err != nil {
		status = 0
		detail = "network"
	}
	db.InsertUpstreamCall(accountID, method, path, status, detail)
}

func callUncertain(err error) bool {
	if err == nil {
		return false
	}
	var api *avanfinity.APIError
	if !errors.As(err, &api) {
		return true
	}
	return api.Status == 408 || api.Status >= 500
}

func clientFor(acc db.CardPlatformAccount) *avanfinity.Client {
	return &avanfinity.Client{Base: acc.SiteBase, AppID: acc.CredPublic, Secret: acc.CredSecret}
}

func loadAccount() (db.XChannel, db.CardPlatformAccount, error) {
	all, err := db.ListXChannels()
	if err != nil {
		return db.XChannel{}, db.CardPlatformAccount{}, err
	}
	var ch db.XChannel
	found := false
	for _, item := range all {
		if item.Channel == db.XChannelCDK {
			ch = item
			found = true
			break
		}
	}
	if !found || !ch.Enabled || ch.AccountID <= 0 {
		return ch, db.CardPlatformAccount{}, fmt.Errorf("先在卡台启用 Avanfinity 的 X CDK。TG 和它共用同一套凭证和钱包")
	}
	acc, err := db.GetCardPlatformAccount(ch.AccountID)
	if err != nil {
		return ch, acc, err
	}
	if !strings.EqualFold(acc.Status, "active") {
		return ch, acc, fmt.Errorf("Avanfinity 卡台已停用")
	}
	return ch, acc, nil
}

func limitsReady(limit db.TGPlanLimit) error {
	if !limit.Enabled {
		return fmt.Errorf("这个套餐已停售")
	}
	if limit.MaxOfficialAmountMinor <= 0 || strings.TrimSpace(limit.Currency) == "" {
		return fmt.Errorf("还没填官方金额上限和币种")
	}
	if _, ok := xmember.USDToE4(limit.MaxWalletDebitUSD); !ok || strings.TrimSpace(limit.MaxWalletDebitUSD) == "" {
		return fmt.Errorf("还没填钱包上限")
	}
	if _, ok := xmember.USDToE4(limit.FundingAmountUSD); !ok || strings.TrimSpace(limit.FundingAmountUSD) == "" {
		return fmt.Errorf("还没填注资金额")
	}
	return nil
}

func Issue(ctx context.Context, plan string, qty int, note, by string) ([]string, error) {
	if !db.KnownTGPlan(plan) {
		return nil, fmt.Errorf("TG 只有 Premium 3、6、12 个月")
	}
	if qty < 1 || qty > 200 {
		return nil, fmt.Errorf("数量要在 1 到 200 之间")
	}
	ch, acc, err := loadAccount()
	if err != nil {
		return nil, err
	}
	limits, err := db.ListTGPlanLimits()
	if err != nil {
		return nil, err
	}
	var limit db.TGPlanLimit
	for _, row := range limits {
		if row.Plan == plan {
			limit = row
		}
	}
	if err := limitsReady(limit); err != nil {
		return nil, err
	}
	var codes []string
	left := qty
	for left > 0 {
		n := left
		if n > 50 {
			n = 50
		}
		part, err := issueBatch(ctx, ch, acc, limit, n, note, by)
		codes = append(codes, part...)
		if err != nil {
			return codes, err
		}
		left -= n
	}
	return codes, nil
}

func issueBatch(ctx context.Context, ch db.XChannel, acc db.CardPlatformAccount, limit db.TGPlanLimit, n int, note, by string) ([]string, error) {
	idem := newUUID()
	wallet, _ := xmember.USDToE4(limit.MaxWalletDebitUSD)
	funding, _ := xmember.USDToE4(limit.FundingAmountUSD)
	body := map[string]any{
		"plan": limit.Plan, "quantity": n,
		"maxWalletDebitUsd": xmember.E4ToUSD(wallet), "maxOfficialAmountMinor": limit.MaxOfficialAmountMinor,
		"currency": strings.ToLower(limit.Currency), "fundingAmountUsd": xmember.E4ToUSD(funding),
	}
	pay, err := xmember.CDKPayFields(ctx, clientFor(acc), ch)
	if err != nil {
		return nil, err
	}
	for k, v := range pay {
		body[k] = v
	}
	raw, _ := json.Marshal(body)
	batchID, err := db.InsertTGBatch(db.TGBatch{
		Plan: limit.Plan, AccountID: acc.ID, Quantity: n, Note: note,
		IdempotencyKey: idem, CreatedBy: by, Status: "pending", RequestJSON: string(raw),
	})
	if err != nil {
		return nil, err
	}
	client := clientFor(acc)
	gen, idem, saved, err := generateTGWithFallback(ctx, client, acc.ID, batchID, idem, body, ch)
	if err != nil {
		return nil, err
	}
	fillIssued(ctx, client, gen.List)
	codes, err := insertLocal(limit, ch, acc.ID, batchID, idem, note, by, gen.List)
	if err != nil {
		return codes, err
	}
	_ = db.SaveTGBatchState(batchID, "issued", saved)
	return codes, nil
}

func generateTGWithFallback(ctx context.Context, client *avanfinity.Client, accountID, batchID int64, idem string, body map[string]any, ch db.XChannel) (*avanfinity.GenerateResult, string, string, error) {
	ids, err := xmember.CDKTryIDs(ctx, client, ch)
	buf, _ := json.Marshal(body)
	raw := string(buf)
	if err != nil {
		return nil, idem, raw, err
	}
	var last error
	for i, id := range ids {
		if i > 0 {
			idem = newUUID()
			body["cardId"] = id
			delete(body, "autoCard")
			buf, _ = json.Marshal(body)
			raw = string(buf)
		}
		_ = db.SaveTGBatchAttempt(batchID, "pending", idem, raw)
		gen, callErr := client.GenerateTGCDKs(ctx, idem, body)
		noteCall(accountID, "POST", "/tg-direct/cdks/generate", callErr)
		if callErr == nil {
			return gen, idem, raw, nil
		}
		last = callErr
		if callUncertain(callErr) || !xmember.CardRefused(callErr) || i == len(ids)-1 {
			break
		}
	}
	if callUncertain(last) {
		return nil, idem, raw, fmt.Errorf("上游发码结果还没确认。请在最近批次里重试第 %d 批，不要重新生成", batchID)
	}
	_ = db.SaveTGBatchAttempt(batchID, "failed", idem, raw)
	if last == nil {
		last = fmt.Errorf("没有发出去")
	}
	return nil, idem, raw, fmt.Errorf("上游发码失败：%w", last)
}

func RetryIssue(ctx context.Context, batchID int64) ([]string, error) {
	b, err := db.GetTGBatch(batchID)
	if err != nil {
		return nil, err
	}
	existing, err := db.ListTGCodesByBatch(batchID)
	if err != nil {
		return nil, err
	}
	if b.Status == "issued" && b.Quantity > 0 && len(existing) >= b.Quantity {
		return siteCodes(existing), nil
	}
	if b.IdempotencyKey == "" || b.RequestJSON == "" {
		return nil, fmt.Errorf("这一批没有可重放的请求")
	}
	acc, err := db.GetCardPlatformAccount(b.AccountID)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(b.RequestJSON), &body); err != nil {
		return nil, fmt.Errorf("批次请求已损坏")
	}
	client := clientFor(acc)
	gen, err := client.GenerateTGCDKs(ctx, b.IdempotencyKey, body)
	noteCall(acc.ID, "POST", "/tg-direct/cdks/generate", err)
	if callUncertain(err) {
		return nil, fmt.Errorf("上游发码结果仍未确认，请稍后再重试这一批")
	}
	if err != nil {
		return nil, fmt.Errorf("上游发码失败：%w", err)
	}
	fillIssued(ctx, client, gen.List)
	ch, _, _ := loadAccount()
	limit := db.TGPlanLimit{
		Plan: b.Plan, Enabled: true,
		Currency: strField(body, "currency"), MaxOfficialAmountMinor: int64(numField(body, "maxOfficialAmountMinor")),
		MaxWalletDebitUSD: strField(body, "maxWalletDebitUsd"), FundingAmountUSD: strField(body, "fundingAmountUsd"),
	}
	if _, err := insertLocal(limit, ch, b.AccountID, b.ID, b.IdempotencyKey, b.Note, b.CreatedBy, gen.List); err != nil {
		return nil, err
	}
	_ = db.SaveTGBatchState(b.ID, "issued", b.RequestJSON)
	all, err := db.ListTGCodesByBatch(b.ID)
	if err != nil {
		return nil, err
	}
	return siteCodes(all), nil
}

func strField(body map[string]any, key string) string {
	s, _ := body[key].(string)
	return s
}
func numField(body map[string]any, key string) float64 {
	n, _ := body[key].(float64)
	return n
}

func siteCodes(rows []db.TGCode) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Code)
	}
	return out
}

func fillIssued(ctx context.Context, client *avanfinity.Client, list []avanfinity.IssuedCDK) {
	for i := range list {
		if list[i].Code != "" || list[i].ID == "" {
			continue
		}
		code, err := client.ExportTGCDK(ctx, list[i].ID)
		noteCall(0, "GET", "/tg-direct/cdks/export", err)
		if err == nil {
			list[i].Code = code
		}
	}
}

func insertLocal(limit db.TGPlanLimit, ch db.XChannel, accountID, batchID int64, idem, note, by string, issued []avanfinity.IssuedCDK) ([]string, error) {
	wallet, _ := xmember.USDToE4(limit.MaxWalletDebitUSD)
	funding, _ := xmember.USDToE4(limit.FundingAmountUSD)
	if len(issued) == 0 {
		return nil, fmt.Errorf("上游没有返回卡密")
	}
	var codes []string
	for _, item := range issued {
		if item.ID != "" {
			exists, err := db.HasTGUpstreamCDK(item.ID)
			if err != nil {
				return codes, err
			}
			if exists {
				continue
			}
		}
		if item.Code == "" {
			return codes, fmt.Errorf("上游没有返回完整卡密，请重试这一批")
		}
		code := newSiteCode()
		prefix := item.CodePrefix
		if prefix == "" && len(item.Code) >= 6 {
			prefix = item.Code[:6]
		}
		if _, err := db.InsertTGCode(db.TGCode{
			Code: code, Plan: limit.Plan, AccountID: accountID, Status: "unused",
			UpstreamCDKID: item.ID, UpstreamCodeEnc: xmember.SealPlain(item.Code), UpstreamCodePrefix: prefix,
			Currency: strings.ToLower(item.Currency), MaxOfficialAmountMinor: item.MaxOfficialAmountMinor,
			MaxWalletDebitE4: wallet, FundingAmountE4: funding, PricingVersion: item.PricingVersion,
			DeviceToken: newToken(32), BatchID: batchID, IdempotencyKey: idem, Note: note, CreatedBy: by, CardID: ch.CardID,
		}); err != nil {
			return codes, err
		}
		codes = append(codes, code)
	}
	return codes, nil
}

func customerView(status string) (step string, reusable bool, headline string) {
	switch status {
	case "unused":
		return "code", true, ""
	case "quoted":
		return "confirm", false, "请确认开通账号"
	case "funding", "funded", "paying", "processing":
		return "progress", false, "正在开通"
	case "paid_pending_delivery":
		return "progress", false, "已付款，等待到账"
	case "completed":
		return "done", false, "开通成功"
	case "review_required", "requires_action", "uncertain":
		return "locked", false, "正在核实付款结果"
	case "disabled":
		return "dead", false, "卡密已失效"
	default:
		return "progress", false, "正在处理"
	}
}

func stateOf(code db.TGCode, red db.TGRedemption, has bool) PublicState {
	step, reusable, headline := customerView(code.Status)
	detail := ""
	recipient := ""
	if has {
		detail = red.Message
		recipient = red.Recipient
		if red.ErrorCode == "SPENDABLE_BALANCE_INSUFFICIENT" {
			headline = "正在排队扣款"
			detail = "钱包暂时不够，系统会自动重试。卡密已锁定，不要重复提交。"
			reusable = false
		}
	}
	if code.Status == "unused" && has && red.FinishedAt != "" && red.UpstreamStatus == "ineligible" {
		step, reusable = "ineligible", true
		headline = "这个账号现在不能接收 Telegram Premium"
		detail = "常见原因：已经是 Premium、用户名刚改过，或账号受限。卡密没有被消耗，换一个用户名再试。"
	}
	st := PublicState{
		Code: code.Code, Plan: code.Plan, PlanLabel: PlanLabel(code.Plan), Status: code.Status,
		Step: step, Headline: headline, Detail: detail, Recipient: recipient, Reusable: reusable,
	}
	if has && (red.UpstreamStatus == "queued_confirm" || red.UpstreamStatus == "queued_quote") && red.FinishedAt == "" {
		st.Queued = true
		st.Step = "queued"
		st.Headline = "排队中"
	}
	return st
}

func Preview(ctx context.Context, raw string) (PublicState, error) {
	code, err := db.GetTGCodeByCode(raw)
	if err != nil {
		return PublicState{}, err
	}
	red, rerr := db.LatestTGRedemption(code.ID)
	has := rerr == nil
	if code.Status == "unused" {
		if acc, err := db.GetCardPlatformAccount(code.AccountID); err == nil {
			plain := xmember.OpenPlain(code.UpstreamCodeEnc)
			if plain != "" {
				_, err = clientFor(acc).PreviewTGCDK(ctx, plain, code.DeviceToken)
				noteCall(acc.ID, "POST", "/public/tg-cdk/preview", err)
			}
		}
	}
	return stateOf(code, red, has), nil
}

func Quote(ctx context.Context, rawCode, handle string) (PublicState, error) {
	name := NormalizeHandle(handle)
	if !ValidHandle(name) {
		return PublicState{}, fmt.Errorf("用户名是 1～32 位字母、数字或下划线，不要填显示名或链接")
	}
	code, err := db.GetTGCodeByCode(rawCode)
	if err != nil {
		return PublicState{}, err
	}
	if code.Status == "disabled" {
		return stateOf(code, db.TGRedemption{}, false), nil
	}
	if code.Status != "unused" && code.Status != "quoted" {
		red, _ := db.LatestTGRedemption(code.ID)
		return stateOf(code, red, red.ID > 0), nil
	}
	red, err := ensureRedemption(ctx, code, name)
	if err != nil {
		return PublicState{}, err
	}
	if err := runQuote(ctx, &code, &red); err != nil {
		return PublicState{}, err
	}
	return stateOf(code, red, true), nil
}

func ensureRedemption(ctx context.Context, code db.TGCode, recipient string) (db.TGRedemption, error) {
	_ = ctx
	red, err := db.LatestTGRedemption(code.ID)
	if err == nil && red.FinishedAt == "" {
		switch xlogic.RecipientAction(red.Recipient, recipient, red.FundingDispatched || red.PaymentAttempted) {
		case xlogic.ActReject:
			return red, fmt.Errorf("这张卡密已经在为 @%s 开通，不能更换账号", red.Recipient)
		case xlogic.ActReplace:
			red.FinishedAt = now()
			red.UpstreamStatus = "cancelled"
			red.Message = "客户更换了用户名，旧报价作废"
			_ = db.SaveTGRedemption(red)
			if !red.FundingDispatched && !red.PaymentAttempted {
				code.Status = "unused"
				_ = db.UpdateTGCodeStatus(code.ID, code.Status)
			}
		default:
			if recipient != "" {
				red.Recipient = recipient
			}
			return red, nil
		}
	}
	red = db.TGRedemption{
		TGCodeID: code.ID, AccountID: code.AccountID, Recipient: recipient,
		ClientRequestID: newUUID(), IdempotencyKey: newUUID(), EventsJSON: "[]",
	}
	id, err := db.InsertTGRedemption(red)
	if err != nil {
		return red, err
	}
	red.ID = id
	return red, nil
}

func runQuote(ctx context.Context, code *db.TGCode, red *db.TGRedemption) error {
	acc, err := db.GetCardPlatformAccount(code.AccountID)
	if err != nil {
		return err
	}
	plain := xmember.OpenPlain(code.UpstreamCodeEnc)
	if plain == "" {
		return fmt.Errorf("这张码缺少上游兑换信息，请联系客服")
	}
	appendEvent(red, "报价 @"+red.Recipient)
	pub, err := clientFor(acc).PreflightTGCDK(ctx, plain, code.DeviceToken, red.Recipient, red.ClientRequestID)
	noteCall(acc.ID, "POST", "/public/tg-cdk/preflight", err)
	if err != nil {
		return hold(ctx, "quote", code, red, err)
	}
	if err := applyPublic(code, red, pub, false); err != nil {
		_ = db.SaveTGRedemption(*red)
		_ = db.UpdateTGCodeStatus(code.ID, code.Status)
		return err
	}
	if code.MaxOfficialAmountMinor > 0 && red.AmountMinor > code.MaxOfficialAmountMinor {
		code.Status = "unused"
		red.FinishedAt = now()
		red.Message = "报价超过发码时锁定的上限，没有扣款"
		_ = db.SaveTGRedemption(*red)
		_ = db.UpdateTGCodeStatus(code.ID, code.Status)
		return fmt.Errorf("报价已变化，请联系客服")
	}
	_ = db.SaveTGRedemption(*red)
	_ = db.UpdateTGCodeStatus(code.ID, code.Status)
	return nil
}

func Confirm(ctx context.Context, raw string) (PublicState, error) {
	code, err := db.GetTGCodeByCode(raw)
	if err != nil {
		return PublicState{}, err
	}
	red, err := db.LatestTGRedemption(code.ID)
	if err != nil || red.FinishedAt != "" {
		return PublicState{}, fmt.Errorf("请先填写 Telegram 用户名")
	}
	if code.Status != "quoted" && code.Status != "funding" && code.Status != "funded" {
		return stateOf(code, red, true), nil
	}
	if err := runConfirm(ctx, &code, &red); err != nil {
		return PublicState{}, err
	}
	return stateOf(code, red, true), nil
}

func runConfirm(ctx context.Context, code *db.TGCode, red *db.TGRedemption) error {
	if code.Status == "funding" || code.Status == "funded" || red.FundingDispatched {
		return refresh(ctx, code, red)
	}
	acc, err := db.GetCardPlatformAccount(code.AccountID)
	if err != nil {
		return err
	}
	client := clientFor(acc)
	plain := xmember.OpenPlain(code.UpstreamCodeEnc)
	appendEvent(red, "客户确认开通")
	pub, err := client.RedeemTGCDK(ctx, plain, code.DeviceToken, red.ClientRequestID, red.Currency, red.AmountMinor)
	noteCall(acc.ID, "POST", "/public/tg-cdk/redeem", err)
	if err != nil {
		return hold(ctx, "pay", code, red, err)
	}
	if err := applyPublic(code, red, pub, true); err != nil {
		_ = db.SaveTGRedemption(*red)
		_ = db.UpdateTGCodeStatus(code.ID, code.Status)
		return err
	}
	if xlogic.ShouldSecondRedeem(channel, code.Status, red.PaymentDispatched) {
		if err := pay(ctx, client, code, red, plain); err != nil {
			return err
		}
	}
	_ = db.SaveTGRedemption(*red)
	_ = db.UpdateTGCodeStatus(code.ID, code.Status)
	return nil
}

func pay(ctx context.Context, client *avanfinity.Client, code *db.TGCode, red *db.TGRedemption, plain string) error {
	if !xlogic.ShouldSecondRedeem(channel, code.Status, red.PaymentDispatched) {
		return nil
	}
	appendEvent(red, "第二次 redeem：派发付款")
	pub, err := client.RedeemTGCDK(ctx, plain, code.DeviceToken, red.ClientRequestID, red.Currency, red.AmountMinor)
	noteCall(code.AccountID, "POST", "/public/tg-cdk/redeem", err)
	if err != nil {
		var api *avanfinity.APIError
		if errors.As(err, &api) && xlogic.QuoteStale(api.ErrorCode) {
			refreshed, perr := client.PreflightTGCDK(ctx, plain, code.DeviceToken, red.Recipient, red.ClientRequestID)
			noteCall(code.AccountID, "POST", "/public/tg-cdk/preflight", perr)
			if perr != nil {
				return hold(ctx, "pay", code, red, perr)
			}
			_ = applyPublic(code, red, refreshed, false)
			pub, err = client.RedeemTGCDK(ctx, plain, code.DeviceToken, red.ClientRequestID, red.Currency, red.AmountMinor)
			noteCall(code.AccountID, "POST", "/public/tg-cdk/redeem", err)
		}
	}
	if err != nil {
		return hold(ctx, "pay", code, red, err)
	}
	red.PaymentDispatched = true
	if err := applyPublic(code, red, pub, true); err != nil {
		_ = db.SaveTGRedemption(*red)
		_ = db.UpdateTGCodeStatus(code.ID, code.Status)
		return err
	}
	_ = db.SaveTGRedemption(*red)
	_ = db.UpdateTGCodeStatus(code.ID, code.Status)
	return nil
}

func applyPublic(code *db.TGCode, red *db.TGRedemption, pub *avanfinity.PublicCDK, fromRedeem bool) error {
	if pub == nil {
		return nil
	}
	if pub.AmountMinor > 0 {
		red.AmountMinor = pub.AmountMinor
	}
	if pub.Currency != "" {
		red.Currency = strings.ToLower(pub.Currency)
	}
	if pub.Recipient != "" {
		got := NormalizeHandle(pub.Recipient)
		if red.Recipient != "" && !strings.EqualFold(got, red.Recipient) {
			red.Message = "上游返回的账号和客户填写的不一致，已停止"
			if fromRedeem || red.FundingDispatched {
				code.Status = "uncertain"
				red.PaymentAttempted = true
				red.FundingDispatched = true
				schedule(code, red)
				return fmt.Errorf("账号不一致，已停止")
			}
			code.Status = "unused"
			red.FinishedAt = now()
			return fmt.Errorf("账号不一致，请重新填写")
		}
		red.Recipient = got
	}
	red.CanRetryPreflight = pub.CanRetryPreflight != nil && *pub.CanRetryPreflight
	// 空错误码不能把 PAY_UNKNOWN 清掉，否则超时未查清时会再发一次付款。
	if pub.ErrorCode != "" || red.ErrorCode != "PAY_UNKNOWN" {
		red.ErrorCode = pub.ErrorCode
	}
	if pub.Message != "" {
		red.Message = pub.Message
	}
	if fromRedeem {
		fund, payMoved := xlogic.NoteFunding(pub.Status, pub.CanRetryPreflight)
		if fund {
			red.FundingDispatched = true
		}
		if payMoved {
			red.PaymentAttempted = true
		}
	}
	var retry *bool
	if pub.CanRetryPreflight != nil {
		retry = pub.CanRetryPreflight
	}
	mapStatus(code, red, pub.Status, retry)
	return nil
}

func mapStatus(code *db.TGCode, red *db.TGRedemption, upstream string, canRetry *bool) {
	d := xlogic.DecideStatus(code.Status, upstream, red.PaymentAttempted, red.FundingDispatched, canRetry)
	if d.CodeStatus != "" {
		code.Status = d.CodeStatus
	}
	if strings.TrimSpace(upstream) != "" {
		red.UpstreamStatus = strings.ToLower(strings.TrimSpace(upstream))
	}
	if d.Release {
		appendEvent(red, "未动钱，卡密放回可用")
	}
	if d.Finish {
		red.FinishedAt = now()
		red.NextPollAt = ""
		return
	}
	if d.Poll {
		schedule(code, red)
		return
	}
	red.NextPollAt = ""
}

func schedule(code *db.TGCode, red *db.TGRedemption) {
	if code.Status == "completed" || code.Status == "disabled" || code.Status == "unused" || code.Status == "quoted" {
		red.NextPollAt = ""
		return
	}
	delays := []time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute}
	d := 5 * time.Minute
	if red.PollCount < len(delays) {
		d = delays[red.PollCount]
	}
	red.NextPollAt = time.Now().Add(d).UTC().Format("2006-01-02 15:04:05")
}

func appendEvent(red *db.TGRedemption, text string) {
	var events []map[string]string
	_ = json.Unmarshal([]byte(red.EventsJSON), &events)
	events = append(events, map[string]string{"t": time.Now().Format("01-02 15:04:05"), "s": text})
	raw, _ := json.Marshal(events)
	red.EventsJSON = string(raw)
}

func hold(ctx context.Context, stage string, code *db.TGCode, red *db.TGRedemption, err error) error {
	_ = ctx
	network := true
	status := 0
	errorCode := ""
	var api *avanfinity.APIError
	if errors.As(err, &api) {
		network = false
		status = api.Status
		errorCode = api.ErrorCode
	}
	switch xlogic.ClassifyFailure(stage, network, status, errorCode) {
	case xlogic.FailRetry:
		red.Message = "暂时没有连上，系统会用原请求重试"
		if stage == "quote" && (red.UpstreamStatus == "" || red.UpstreamStatus == "queued_quote") {
			red.UpstreamStatus = "queued_quote"
		}
		red.NextPollAt = time.Now().Add(15 * time.Second).UTC().Format("2006-01-02 15:04:05")
		appendEvent(red, "未成功，稍后重试")
		_ = db.SaveTGRedemption(*red)
		return nil
	case xlogic.FailWait:
		red.Message = "开通人数较多，系统会自动继续"
		red.NextPollAt = time.Now().Add(60 * time.Second).UTC().Format("2006-01-02 15:04:05")
		_ = db.SaveTGRedemption(*red)
		return nil
	case xlogic.FailBalance:
		code.Status = "funding"
		red.ErrorCode = "SPENDABLE_BALANCE_INSUFFICIENT"
		red.Message = "钱包余额不足，充值后会用原请求继续"
		schedule(code, red)
		_ = db.SaveTGRedemption(*red)
		_ = db.UpdateTGCodeStatus(code.ID, code.Status)
		return nil
	case xlogic.FailStale:
		return err
	case xlogic.FailUncertain:
		code.Status = "uncertain"
		red.ErrorCode = "PAY_UNKNOWN"
		red.PaymentAttempted = true
		red.FundingDispatched = true
		red.Message = "付款结果还没确认，卡密已锁定，不会重新支付"
		appendEvent(red, "付款结果不确定，改为只查询")
		schedule(code, red)
		_ = db.SaveTGRedemption(*red)
		_ = db.UpdateTGCodeStatus(code.ID, code.Status)
		return nil
	default:
		return err
	}
}

func Result(raw string) (PublicState, error) {
	code, err := db.GetTGCodeByCode(raw)
	if err != nil {
		return PublicState{}, err
	}
	red, rerr := db.LatestTGRedemption(code.ID)
	return stateOf(code, red, rerr == nil), nil
}

func refresh(ctx context.Context, code *db.TGCode, red *db.TGRedemption) error {
	acc, err := db.GetCardPlatformAccount(code.AccountID)
	if err != nil {
		return err
	}
	plain := xmember.OpenPlain(code.UpstreamCodeEnc)
	wasUnknown := red.ErrorCode == "PAY_UNKNOWN"
	pub, err := clientFor(acc).ResultTGCDK(ctx, plain, code.DeviceToken, red.ClientRequestID)
	noteCall(acc.ID, "POST", "/public/tg-cdk/result", err)
	if err != nil {
		return hold(ctx, "read", code, red, err)
	}
	if err := applyPublic(code, red, pub, false); err != nil {
		_ = db.SaveTGRedemption(*red)
		_ = db.UpdateTGCodeStatus(code.ID, code.Status)
		return err
	}
	if wasUnknown && code.Status != "uncertain" && red.ErrorCode == "PAY_UNKNOWN" {
		red.ErrorCode = pub.ErrorCode
	}
	// 上一次付款调用超时还没查清时，不能再发第二次 redeem。
	if xlogic.ShouldSecondRedeem(channel, code.Status, red.PaymentDispatched) && red.ErrorCode != "PAY_UNKNOWN" {
		if err := pay(ctx, clientFor(acc), code, red, plain); err != nil {
			return err
		}
	}
	_ = db.SaveTGRedemption(*red)
	_ = db.UpdateTGCodeStatus(code.ID, code.Status)
	return nil
}

func PollDue(ctx context.Context) {
	rows, err := db.DueTGRedemptions(20)
	if err != nil || len(rows) == 0 {
		return
	}
	for _, red := range rows {
		if !tryRed(red.ID) {
			continue
		}
		func() {
			defer unlockRed(red.ID)
			code, err := db.GetTGCode(red.TGCodeID)
			if err != nil {
				return
			}
			red.PollCount++
			switch xlogic.PollAction(code.Status, red.UpstreamStatus, "") {
			case xlogic.ActionConfirm:
				_ = runConfirm(ctx, &code, &red)
			case xlogic.ActionQuote:
				_ = runQuote(ctx, &code, &red)
			case xlogic.ActionRefresh:
				_ = refresh(ctx, &code, &red)
			default:
				red.NextPollAt = ""
			}
			_ = db.SaveTGRedemption(red)
			_ = db.UpdateTGCodeStatus(code.ID, code.Status)
		}()
	}
}

func Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("[tg-member] poll panic: %v", r)
						}
					}()
					PollDue(ctx)
				}()
			}
		}
	}()
}

func DisableCode(ctx context.Context, id int64) error {
	code, err := db.GetTGCode(id)
	if err != nil {
		return err
	}
	if code.Status != "unused" {
		return fmt.Errorf("只有未使用的码可以作废")
	}
	if code.UpstreamCDKID != "" {
		acc, err := db.GetCardPlatformAccount(code.AccountID)
		if err != nil {
			return err
		}
		err = clientFor(acc).RevokeTGCDK(ctx, code.UpstreamCDKID, newUUID())
		noteCall(acc.ID, "POST", "/tg-direct/cdks/revoke", err)
		if err != nil {
			return fmt.Errorf("上游撤销失败：%w", err)
		}
	}
	return db.UpdateTGCodeStatus(id, "disabled")
}

// Ready 说明 TG 能不能发码。它不单独配凭证，必须先启用 Avanfinity 的 X CDK 通道。
func Ready() error {
	_, _, err := loadAccount()
	return err
}

func Requery(ctx context.Context, redemptionID int64) error {
	red, err := db.GetTGRedemption(redemptionID)
	if err != nil {
		return err
	}
	code, err := db.GetTGCode(red.TGCodeID)
	if err != nil {
		return err
	}
	return refresh(ctx, &code, &red)
}

func Resolve(ctx context.Context, id int64, outcome, note string) error {
	_ = ctx
	red, err := db.GetTGRedemption(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("没有找到这张兑换记录")
		}
		return err
	}
	code, err := db.GetTGCode(red.TGCodeID)
	if err != nil {
		return err
	}
	switch outcome {
	case "completed":
		code.Status = "completed"
		red.FinishedAt = now()
		red.ResolvedAt = now()
		red.ResolvedNote = note
		appendEvent(&red, "人工确认已开通")
	case "release":
		if red.FundingDispatched || red.PaymentAttempted {
			return fmt.Errorf("可能已经扣过款，不能放回未使用")
		}
		code.Status = "unused"
		red.FinishedAt = now()
		red.ResolvedAt = now()
		red.ResolvedNote = note
		appendEvent(&red, "人工退回，客户可重新提交")
	case "disable":
		if code.Status == "completed" {
			return fmt.Errorf("已开通的码不能作废")
		}
		code.Status = "disabled"
		red.ResolvedAt = now()
		red.ResolvedNote = note
		appendEvent(&red, "人工作废")
	default:
		return fmt.Errorf("未知处理结果")
	}
	if err := db.SaveTGRedemption(red); err != nil {
		return err
	}
	return db.UpdateTGCodeStatus(code.ID, code.Status)
}
