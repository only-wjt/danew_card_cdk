package xmember

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/danew/cdk-recharge-system/internal/avanfinity"
	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/danew/cdk-recharge-system/internal/notify"
	"github.com/danew/cdk-recharge-system/internal/xmember/xlogic"
)

// PublicState 是客户页只需要的字段。没有通道、外币金额和上游码。
type PublicState struct {
	Code       string `json:"code"`
	Plan       string `json:"plan"`
	PlanLabel  string `json:"plan_label"`
	Status     string `json:"status"`
	Step       string `json:"step"`
	Headline   string `json:"headline"`
	Detail     string `json:"detail"`
	Recipient  string `json:"recipient"`
	Reusable   bool   `json:"reusable"`
	Queued     bool   `json:"queued"`
	QueueAhead int    `json:"queue_ahead"`
}

var errRecipientMismatch = errors.New("订单账号和客户填写的不一致")

var (
	rateMu   sync.Mutex
	rateHits = map[string][]time.Time{}
	redLocks sync.Map
)

func planLabel(plan string) string {
	switch plan {
	case "premium_3m":
		return "X Premium · 3 个月"
	case "premium_6m":
		return "X Premium · 6 个月"
	case "premium_12m":
		return "X Premium · 12 个月"
	case "premium_plus_3m":
		return "X Premium+ · 3 个月"
	case "premium_plus_6m":
		return "X Premium+ · 6 个月"
	case "premium_plus_12m":
		return "X Premium+ · 12 个月"
	default:
		return plan
	}
}

func clientFor(acc db.CardPlatformAccount) *avanfinity.Client {
	return &avanfinity.Client{Base: acc.SiteBase, AppID: acc.CredPublic, Secret: acc.CredSecret}
}

func channelByName(channel string) (db.XChannel, error) {
	all, err := db.ListXChannels()
	if err != nil {
		return db.XChannel{}, err
	}
	for _, item := range all {
		if item.Channel == channel {
			return item, nil
		}
	}
	return db.XChannel{}, fmt.Errorf("通道不存在")
}

func loadChannel(channel string) (db.XChannel, db.CardPlatformAccount, error) {
	ch, err := channelByName(channel)
	if err != nil {
		return ch, db.CardPlatformAccount{}, err
	}
	if ch.AccountID <= 0 {
		return ch, db.CardPlatformAccount{}, fmt.Errorf("通道还没绑定卡台")
	}
	acc, err := db.GetCardPlatformAccount(ch.AccountID)
	if err != nil {
		return ch, acc, err
	}
	if !strings.EqualFold(acc.Status, "active") {
		return ch, acc, fmt.Errorf("卡台已停用")
	}
	return ch, acc, nil
}

// accountForCode 兑换过程只用发码时写死的账户，不跟通道当前绑定走。
func accountForCode(code db.XCode) (db.CardPlatformAccount, error) {
	if code.AccountID <= 0 {
		return db.CardPlatformAccount{}, fmt.Errorf("这张码没有绑定卡台")
	}
	acc, err := db.GetCardPlatformAccount(code.AccountID)
	if err != nil {
		return acc, err
	}
	if !strings.EqualFold(acc.Status, "active") {
		return acc, fmt.Errorf("卡台已停用")
	}
	return acc, nil
}

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

func moneyMoved(red db.XRedemption) bool {
	return red.PaymentAttempted || red.FundingDispatched || red.PaymentDispatched
}

func sameHandle(a, b string) bool {
	return strings.EqualFold(NormalizeHandle(a), NormalizeHandle(b))
}

func rateBucket(kind string, accountID int64) (string, int) {
	switch kind {
	case "preflight":
		return "preflight", 10
	case "redeem":
		return "redeem", 10
	case "prepare":
		return fmt.Sprintf("prepare:%d", accountID), 5
	case "result":
		return "result", 100
	case "preview":
		return "preview", 60
	default:
		return fmt.Sprintf("%s:%d", kind, accountID), 10
	}
}

func acquire(kind string, accountID int64) bool {
	key, limit := rateBucket(kind, accountID)
	rateMu.Lock()
	defer rateMu.Unlock()
	ok, next := xlogic.AllowRate(rateHits[key], time.Now(), limit, time.Minute)
	rateHits[key] = next
	return ok
}

func tryRed(id int64) bool {
	_, loaded := redLocks.LoadOrStore(id, struct{}{})
	return !loaded
}

func unlockRed(id int64) { redLocks.Delete(id) }

// gate 限流不通过时把这条兑换排进真实队列，不调用上游。
func gate(kind string, accountID int64, red *db.XRedemption, queueStatus string) bool {
	if acquire(kind, accountID) {
		return true
	}
	if queueStatus != "" {
		red.UpstreamStatus = queueStatus
		red.Message = "排队中，轮到后会自动继续"
	}
	red.NextPollAt = time.Now().Add(60 * time.Second).UTC().Format("2006-01-02 15:04:05")
	_ = db.SaveXRedemption(*red)
	return false
}

func limitsReady(channel string, limit db.XPlanLimit) error {
	if !limit.Enabled {
		return fmt.Errorf("这个套餐没启用")
	}
	if limit.MaxOfficialAmountMinor <= 0 || strings.TrimSpace(limit.Currency) == "" {
		return fmt.Errorf("先在通道设置里填好这个套餐的币种和花费上限")
	}
	if channel == db.XChannelCDK {
		wallet, _ := USDToE4(limit.MaxWalletDebitUSD)
		if wallet <= 0 {
			return fmt.Errorf("先在通道设置里填好这个套餐的币种和花费上限")
		}
		// 钱包授权要覆盖充值本金加全部费用，比本金还小时上游一定 409，提前挡住。
		if funding, _ := USDToE4(limit.FundingAmountUSD); funding > wallet {
			return fmt.Errorf("钱包授权上限 $%s 小于充值本金 $%s，上游会拒绝。授权上限要不低于本金加全部手续费", E4ToUSD(wallet), E4ToUSD(funding))
		}
	}
	return nil
}

// Issue 发一批本站码。CDK 会调用上游生成；直充只在本站落码。数量按 50 拆批。
func Issue(ctx context.Context, plan, channel string, qty int, note, by string) ([]string, error) {
	if qty < 1 || qty > 200 {
		return nil, fmt.Errorf("数量要在 1 到 200 之间")
	}
	ch, acc, err := loadChannel(channel)
	if err != nil {
		return nil, err
	}
	if !ch.Enabled {
		return nil, fmt.Errorf("通道没启用")
	}
	limits, err := db.ListXPlanLimits(channel)
	if err != nil {
		return nil, err
	}
	var limit db.XPlanLimit
	for _, row := range limits {
		if row.Plan == plan {
			limit = row
		}
	}
	if err := limitsReady(channel, limit); err != nil {
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

func issueBatch(ctx context.Context, ch db.XChannel, acc db.CardPlatformAccount, limit db.XPlanLimit, n int, note, by string) ([]string, error) {
	idem := NewUUID()
	var body map[string]any
	var rawBody string
	status := "issued"
	if ch.Channel == db.XChannelCDK {
		status = "pending"
		wallet, _ := USDToE4(limit.MaxWalletDebitUSD)
		funding, _ := USDToE4(limit.FundingAmountUSD)
		body = map[string]any{
			"plan": limit.Plan, "quantity": n,
			"maxWalletDebitUsd": E4ToUSD(wallet), "maxOfficialAmountMinor": limit.MaxOfficialAmountMinor,
			"currency": strings.ToLower(limit.Currency), "fundingAmountUsd": E4ToUSD(funding),
		}
		pay, err := CDKPayFields(ctx, clientFor(acc), ch)
		if err != nil {
			return nil, err
		}
		for k, v := range pay {
			body[k] = v
		}
		buf, _ := json.Marshal(body)
		rawBody = string(buf)
	}
	batchID, err := db.InsertXBatch(db.XBatch{
		Plan: limit.Plan, Channel: ch.Channel, AccountID: acc.ID, Quantity: n,
		Note: note, IdempotencyKey: idem, CreatedBy: by, Status: status, RequestJSON: rawBody,
	})
	if err != nil {
		return nil, err
	}
	if ch.Channel == db.XChannelDirect {
		return insertLocalCodes(ctx, nil, limit, ch, acc.ID, batchID, idem, note, by, n, nil)
	}
	client := clientFor(acc)
	gen, idem, rawBody, err := generateXWithFallback(ctx, client, acc.ID, batchID, idem, body, ch)
	if err != nil {
		return nil, err
	}
	fillIssuedCodes(ctx, client, gen.List)
	codes, err := insertLocalCodes(ctx, client, limit, ch, acc.ID, batchID, idem, note, by, n, gen.List)
	if err != nil {
		return codes, err
	}
	_ = db.SaveXBatchState(batchID, "issued", rawBody)
	return codes, nil
}

func generateXWithFallback(ctx context.Context, client *avanfinity.Client, accountID, batchID int64, idem string, body map[string]any, ch db.XChannel) (*avanfinity.GenerateResult, string, string, error) {
	ids, err := CDKTryIDs(ctx, client, ch)
	raw := mustJSON(body)
	if err != nil {
		return nil, idem, raw, err
	}
	var last error
	for i, id := range ids {
		if i > 0 {
			idem = NewUUID()
			body["cardId"] = id
			delete(body, "autoCard")
			raw = mustJSON(body)
		}
		_ = db.SaveXBatchAttempt(batchID, "pending", idem, raw)
		gen, callErr := client.GenerateCDKs(ctx, idem, body)
		noteCall(accountID, "POST", "/x-direct/cdks/generate", callErr)
		if callErr == nil {
			return gen, idem, raw, nil
		}
		last = callErr
		if callUncertain(callErr) || !CardRefused(callErr) || i == len(ids)-1 {
			break
		}
	}
	if callUncertain(last) {
		return nil, idem, raw, fmt.Errorf("上游发码结果还没确认。请在最近批次里重试第 %d 批，不要重新生成", batchID)
	}
	_ = db.SaveXBatchAttempt(batchID, "failed", idem, raw)
	if last == nil {
		last = fmt.Errorf("没有发出去")
	}
	return nil, idem, raw, fmt.Errorf("上游发码失败：%w", last)
}

func mustJSON(body map[string]any) string {
	buf, _ := json.Marshal(body)
	return string(buf)
}

func RetryIssue(ctx context.Context, batchID int64) ([]string, error) {
	b, err := db.GetXBatch(batchID)
	if err != nil {
		return nil, err
	}
	if b.Channel != db.XChannelCDK {
		return nil, fmt.Errorf("直充批次没有上游发码，不用重试")
	}
	existing, err := db.ListXCodesByBatch(batchID)
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
	gen, err := client.GenerateCDKs(ctx, b.IdempotencyKey, body)
	noteCall(acc.ID, "POST", "/x-direct/cdks/generate", err)
	if callUncertain(err) {
		return nil, fmt.Errorf("上游发码结果仍未确认，请稍后再重试这一批，不要重新生成")
	}
	if err != nil {
		return nil, fmt.Errorf("上游发码失败：%w", err)
	}
	fillIssuedCodes(ctx, client, gen.List)
	ch, _ := channelByName(b.Channel)
	ch.Channel = b.Channel
	ch.CardID = cardIDFromBody(body)
	limit := limitFromBody(b.Plan, body)
	if _, err := insertLocalCodes(ctx, client, limit, ch, b.AccountID, b.ID, b.IdempotencyKey, b.Note, b.CreatedBy, b.Quantity, gen.List); err != nil {
		return nil, err
	}
	_ = db.SaveXBatchState(b.ID, "issued", b.RequestJSON)
	all, err := db.ListXCodesByBatch(b.ID)
	if err != nil {
		return nil, err
	}
	return siteCodes(all), nil
}

func siteCodes(rows []db.XCode) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Code)
	}
	return out
}

func cardIDFromBody(body map[string]any) int64 {
	switch n := body["cardId"].(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	default:
		return 0
	}
}

func limitFromBody(plan string, body map[string]any) db.XPlanLimit {
	lim := db.XPlanLimit{Plan: plan, Enabled: true, Currency: strField(body, "currency")}
	lim.MaxWalletDebitUSD = strField(body, "maxWalletDebitUsd")
	lim.FundingAmountUSD = strField(body, "fundingAmountUsd")
	switch n := body["maxOfficialAmountMinor"].(type) {
	case float64:
		lim.MaxOfficialAmountMinor = int64(n)
	case int64:
		lim.MaxOfficialAmountMinor = n
	}
	return lim
}

func strField(body map[string]any, key string) string {
	s, _ := body[key].(string)
	return s
}

func fillIssuedCodes(ctx context.Context, client *avanfinity.Client, list []avanfinity.IssuedCDK) {
	if client == nil {
		return
	}
	for i := range list {
		if list[i].Code != "" || list[i].ID == "" {
			continue
		}
		code, err := client.ExportCDK(ctx, list[i].ID)
		noteCall(0, "GET", "/x-direct/cdks/export", err)
		if err == nil {
			list[i].Code = code
		}
	}
}

func insertLocalCodes(ctx context.Context, client *avanfinity.Client, limit db.XPlanLimit, ch db.XChannel, accountID, batchID int64, idem, note, by string, n int, issued []avanfinity.IssuedCDK) ([]string, error) {
	_ = ctx
	_ = client
	wallet, _ := USDToE4(limit.MaxWalletDebitUSD)
	funding, _ := USDToE4(limit.FundingAmountUSD)
	fee, _ := USDToE4(limit.MaxServiceFeeUSD)
	var codes []string
	if ch.Channel == db.XChannelCDK {
		if len(issued) == 0 {
			return nil, fmt.Errorf("上游没有返回卡密")
		}
		for _, item := range issued {
			if item.ID != "" {
				exists, err := db.HasXUpstreamCDK(item.ID)
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
			code := NewSiteCode()
			prefix := item.CodePrefix
			if prefix == "" && len(item.Code) >= 6 {
				prefix = item.Code[:6]
			}
			if _, err := db.InsertXCode(db.XCode{
				Code: code, Plan: limit.Plan, Channel: ch.Channel, AccountID: accountID, Status: "unused",
				UpstreamCDKID: item.ID, UpstreamCodeEnc: seal(item.Code), UpstreamCodePrefix: prefix,
				Currency: strings.ToLower(item.Currency), MaxOfficialAmountMinor: item.MaxOfficialAmountMinor,
				MaxWalletDebitE4: wallet, FundingAmountE4: funding, ServiceFeeE4: fee,
				PricingVersion: item.PricingVersion, DeviceToken: NewDeviceToken(),
				BatchID: batchID, IdempotencyKey: idem, Note: note, CreatedBy: by, CardID: ch.CardID,
			}); err != nil {
				return codes, err
			}
			codes = append(codes, code)
		}
		return codes, nil
	}
	for i := 0; i < n; i++ {
		code := NewSiteCode()
		if _, err := db.InsertXCode(db.XCode{
			Code: code, Plan: limit.Plan, Channel: ch.Channel, AccountID: accountID, Status: "unused",
			Currency: strings.ToLower(limit.Currency), MaxOfficialAmountMinor: limit.MaxOfficialAmountMinor,
			MaxWalletDebitE4: wallet, FundingAmountE4: funding, ServiceFeeE4: fee,
			DeviceToken: NewDeviceToken(), BatchID: batchID, IdempotencyKey: idem, Note: note, CreatedBy: by,
			CardID: ch.CardID,
		}); err != nil {
			return codes, err
		}
		codes = append(codes, code)
	}
	return codes, nil
}

func stateOf(code db.XCode, red db.XRedemption, hasRed bool) PublicState {
	status := code.Status
	step, reusable, headline := CustomerView(status)
	detail := ""
	if hasRed {
		detail = red.Message
		if red.ErrorCode == "SPENDABLE_BALANCE_INSUFFICIENT" {
			headline = "正在排队扣款"
			detail = "钱包暂时不够，系统会自动重试。卡密已锁定，不要重复提交。"
			reusable = false
		}
	}
	if status == "unused" && hasRed && red.FinishedAt != "" && red.UpstreamStatus == "ineligible" {
		step, reusable = "ineligible", true
		headline = "这个账号现在不能接收礼品会员"
		detail = "常见原因：已经是 Premium、账号受限或刚改过用户名。卡密没有被消耗，换一个账号再试即可。"
	}
	recipient := ""
	if hasRed {
		recipient = red.Recipient
	}
	st := PublicState{
		Code: code.Code, Plan: code.Plan, PlanLabel: planLabel(code.Plan), Status: status,
		Step: step, Headline: headline, Detail: detail, Recipient: recipient, Reusable: reusable,
	}
	if hasRed && (red.UpstreamStatus == "queued_confirm" || red.UpstreamStatus == "queued_quote") && red.FinishedAt == "" {
		st.Queued = true
		st.Step = "queued"
		st.Headline = "排队中"
		if n, err := db.CountXQueueAhead(red.ID); err == nil {
			st.QueueAhead = n
		}
	}
	return st
}

// Preview 只查这张码。CDK 会顺手问上游 preview，失败不锁码。
func Preview(ctx context.Context, raw string) (PublicState, error) {
	code, err := db.GetXCodeByCode(strings.TrimSpace(raw))
	if err != nil {
		return PublicState{}, err
	}
	red, rerr := db.LatestXRedemption(code.ID)
	has := rerr == nil
	if code.Channel == db.XChannelCDK && code.Status == "unused" {
		acc, err := accountForCode(code)
		if err == nil && acquire("preview", acc.ID) {
			plain := openSeal(code.UpstreamCodeEnc)
			if plain != "" {
				_, err = clientFor(acc).PreviewCDK(ctx, plain, code.DeviceToken)
				noteCall(acc.ID, "POST", "/public/x-cdk/preview", err)
			}
		}
	}
	return stateOf(code, red, has), nil
}

// Quote 校验用户名并向上游报价。换账号会取消旧报价并换新的请求号。
func Quote(ctx context.Context, rawCode, handle string) (PublicState, error) {
	name := NormalizeHandle(handle)
	if !ValidHandle(name) {
		return PublicState{}, fmt.Errorf("用户名只能是 1–15 位字母、数字或下划线")
	}
	code, err := db.GetXCodeByCode(strings.TrimSpace(rawCode))
	if err != nil {
		return PublicState{}, err
	}
	if code.Status == "disabled" {
		return stateOf(code, db.XRedemption{}, false), nil
	}
	if code.Status != "unused" && code.Status != "quoted" {
		red, _ := db.LatestXRedemption(code.ID)
		return stateOf(code, red, red.ID > 0), nil
	}
	red, err := ensureRedemption(ctx, code, name)
	if err != nil {
		return PublicState{}, err
	}
	kind := "prepare"
	if code.Channel == db.XChannelCDK {
		kind = "preflight"
	}
	if !gate(kind, code.AccountID, &red, "queued_quote") {
		return stateOf(code, red, true), nil
	}
	if err := runQuote(ctx, &code, &red, true); err != nil {
		return PublicState{}, err
	}
	return stateOf(code, red, true), nil
}

func ensureRedemption(ctx context.Context, code db.XCode, recipient string) (db.XRedemption, error) {
	red, err := db.LatestXRedemption(code.ID)
	if err == nil && red.FinishedAt == "" {
		switch xlogic.RecipientAction(red.Recipient, recipient, moneyMoved(red)) {
		case xlogic.ActReject:
			return red, fmt.Errorf("这张卡密已经在为 @%s 开通，不能更换账号", red.Recipient)
		case xlogic.ActReplace:
			if err := retireQuote(ctx, &code, &red); err != nil {
				return red, err
			}
		default:
			if recipient != "" {
				red.Recipient = recipient
			}
			return red, nil
		}
	}
	red = db.XRedemption{
		XCodeID: code.ID, Channel: code.Channel, AccountID: code.AccountID, Recipient: recipient,
		ClientRequestID: NewUUID(), IdempotencyKey: NewUUID(),
	}
	id, err := db.InsertXRedemption(red)
	if err != nil {
		return red, err
	}
	red.ID = id
	return red, nil
}

func retireQuote(ctx context.Context, code *db.XCode, red *db.XRedemption) error {
	if moneyMoved(*red) {
		return fmt.Errorf("这张卡密已经在付款，不能更换账号")
	}
	if code.Channel == db.XChannelDirect && red.UpstreamOrderID != "" {
		acc, err := accountForCode(*code)
		if err != nil {
			return err
		}
		client := clientFor(acc)
		err = client.CancelDirect(ctx, red.UpstreamOrderID, NewUUID())
		noteCall(acc.ID, "POST", "/x-direct/orders/cancel", err)
		if callUncertain(err) {
			code.Status = "uncertain"
			red.Message = "取消旧报价的结果还没确认，不会另建订单"
			red.ErrorCode = "PAY_UNKNOWN"
			schedule(code, red)
			_ = db.SaveXRedemption(*red)
			_ = db.UpdateXCodeStatus(code.ID, code.Status)
			return fmt.Errorf("取消旧报价的结果还没确认，卡密已锁定")
		}
	}
	red.FinishedAt = now()
	red.NextPollAt = ""
	red.UpstreamStatus = "cancelled"
	red.Message = "客户更换了账号，旧报价已取消"
	appendEvent(red, "更换账号，取消旧报价")
	return db.SaveXRedemption(*red)
}

func payCardID(code db.XCode) (int64, error) {
	if code.CardID > 0 {
		return code.CardID, nil
	}
	ch, err := channelByName(code.Channel)
	if err != nil {
		return 0, err
	}
	if ch.AccountID != code.AccountID {
		return 0, fmt.Errorf("这张码绑定的卡台已经换过，不能用新卡台的付款卡")
	}
	if ch.CardID <= 0 {
		return 0, fmt.Errorf("这张码没有付款卡")
	}
	return ch.CardID, nil
}

func runQuote(ctx context.Context, code *db.XCode, red *db.XRedemption, allowRequote bool) error {
	acc, err := accountForCode(*code)
	if err != nil {
		return err
	}
	client := clientFor(acc)
	appendEvent(red, "报价 @"+red.Recipient)
	if code.Channel == db.XChannelCDK {
		plain := openSeal(code.UpstreamCodeEnc)
		if plain == "" {
			return fmt.Errorf("这张码缺少上游兑换信息，请联系客服")
		}
		pub, err := client.PreflightCDK(ctx, plain, code.DeviceToken, red.Recipient, red.ClientRequestID)
		noteCall(acc.ID, "POST", "/public/x-cdk/preflight", err)
		if err != nil {
			return holdOrQueue(ctx, "quote", code, red, err)
		}
		if err := applyPublic(code, red, pub, false); err != nil {
			_ = db.SaveXRedemption(*red)
			_ = db.UpdateXCodeStatus(code.ID, code.Status)
			return err
		}
	} else {
		cardID, err := payCardID(*code)
		if err != nil {
			return err
		}
		order, err := client.PrepareDirect(ctx, red.IdempotencyKey, map[string]any{
			"recipient": red.Recipient, "plan": code.Plan, "cardId": cardID, "clientRequestId": red.ClientRequestID,
		})
		noteCall(acc.ID, "POST", "/x-direct/orders/prepare", err)
		if err != nil {
			if allowRequote && staleErr(err) && !moneyMoved(*red) {
				return requote(ctx, code, red)
			}
			return holdOrQueue(ctx, "quote", code, red, err)
		}
		if allowRequote && order != nil && xlogic.QuoteStale(order.ErrorCode) && !order.PaymentAttempted && !moneyMoved(*red) {
			return requote(ctx, code, red)
		}
		if err := applyDirect(code, red, order); err != nil {
			if errors.Is(err, errRecipientMismatch) && !order.PaymentAttempted {
				cancelDirect(ctx, client, red)
				code.Status = "unused"
				red.FinishedAt = now()
				red.UpstreamStatus = "cancelled"
				red.Message = "报价账号不一致，已取消"
				_ = db.SaveXRedemption(*red)
				_ = db.UpdateXCodeStatus(code.ID, code.Status)
				return fmt.Errorf("报价账号不一致，已取消")
			}
			_ = db.SaveXRedemption(*red)
			_ = db.UpdateXCodeStatus(code.ID, code.Status)
			return err
		}
		if err := enforceDirectCap(ctx, client, code, red, order); err != nil {
			_ = db.SaveXRedemption(*red)
			_ = db.UpdateXCodeStatus(code.ID, code.Status)
			return err
		}
	}
	_ = db.InsertQuoteSample(code.Channel, code.Plan, red.Currency, "redeem", red.AmountMinor, red.EstimatedUSDE4, red.ServiceFeeE4, red.PricingVersion)
	_ = db.SaveXRedemption(*red)
	_ = db.UpdateXCodeStatus(code.ID, code.Status)
	return nil
}

func enforceDirectCap(ctx context.Context, client *avanfinity.Client, code *db.XCode, red *db.XRedemption, order *avanfinity.DirectOrder) error {
	if code.MaxOfficialAmountMinor <= 0 || strings.TrimSpace(code.Currency) == "" {
		cancelDirect(ctx, client, red)
		code.Status = "unused"
		red.FinishedAt = now()
		red.UpstreamStatus = "cancelled"
		red.Message = "这个套餐还没设置花费上限"
		return fmt.Errorf("暂时无法开通")
	}
	over := !strings.EqualFold(code.Currency, order.Currency)
	over = over || order.AmountMinor > code.MaxOfficialAmountMinor
	fee, _ := USDToE4(order.ServiceFee)
	if code.ServiceFeeE4 > 0 && fee > code.ServiceFeeE4 {
		over = true
	}
	if !over {
		return nil
	}
	cancelDirect(ctx, client, red)
	if order.PaymentAttempted {
		code.Status = "uncertain"
		red.PaymentAttempted = true
	} else {
		code.Status = "unused"
		red.FinishedAt = now()
		red.UpstreamStatus = "cancelled"
	}
	red.Message = "报价超过本站上限，已取消"
	return fmt.Errorf("暂时无法开通")
}

func cancelDirect(ctx context.Context, client *avanfinity.Client, red *db.XRedemption) {
	if red.UpstreamOrderID == "" {
		return
	}
	err := client.CancelDirect(ctx, red.UpstreamOrderID, NewUUID())
	noteCall(red.AccountID, "POST", "/x-direct/orders/cancel", err)
}

// Confirm 客户点一次确认。直充 confirm 一次；CDK 第一次 redeem 注资，funded 后自动第二次。
func Confirm(ctx context.Context, raw string) (PublicState, error) {
	code, err := db.GetXCodeByCode(strings.TrimSpace(raw))
	if err != nil {
		return PublicState{}, err
	}
	red, err := db.LatestXRedemption(code.ID)
	if err != nil || red.FinishedAt != "" {
		return PublicState{}, fmt.Errorf("请先填写 X 账号")
	}
	if code.Status != "quoted" && code.Status != "funding" && code.Status != "funded" {
		return stateOf(code, red, true), nil
	}
	kind := "confirm"
	if code.Channel == db.XChannelCDK {
		kind = "redeem"
	}
	if !gate(kind, code.AccountID, &red, "queued_confirm") {
		return stateOf(code, red, true), nil
	}
	if err := runConfirm(ctx, &code, &red); err != nil {
		return PublicState{}, err
	}
	return stateOf(code, red, true), nil
}

func runConfirm(ctx context.Context, code *db.XCode, red *db.XRedemption) error {
	if code.Channel == db.XChannelCDK && (code.Status == "funding" || code.Status == "funded" || red.FundingDispatched) {
		return refresh(ctx, code, red)
	}
	if code.Channel == db.XChannelDirect && (red.PaymentAttempted || (code.Status != "quoted" && code.Status != "unused")) {
		return refresh(ctx, code, red)
	}
	acc, err := accountForCode(*code)
	if err != nil {
		return err
	}
	client := clientFor(acc)
	appendEvent(red, "客户确认开通")
	if code.Channel == db.XChannelDirect {
		if red.UpstreamOrderID != "" {
			current, err := client.GetDirectOrder(ctx, red.UpstreamOrderID)
			noteCall(acc.ID, "GET", "/x-direct/orders/"+red.UpstreamOrderID, err)
			if err == nil && current != nil && current.Recipient != "" && !sameHandle(current.Recipient, red.Recipient) {
				if !current.PaymentAttempted {
					cancelDirect(ctx, client, red)
					code.Status = "unused"
					red.FinishedAt = now()
					red.Message = "订单账号和当前填写的不一致，已取消"
					_ = db.SaveXRedemption(*red)
					_ = db.UpdateXCodeStatus(code.ID, code.Status)
					return fmt.Errorf("订单账号不一致，已取消，请重新报价")
				}
				code.Status = "uncertain"
				red.PaymentAttempted = true
				red.Message = "订单账号不一致，已停止付款"
				schedule(code, red)
				_ = db.SaveXRedemption(*red)
				_ = db.UpdateXCodeStatus(code.ID, code.Status)
				return fmt.Errorf("订单账号不一致，已停止付款")
			}
		}
		order, err := client.ConfirmDirect(ctx, red.UpstreamOrderID, red.IdempotencyKey, red.AmountMinor, strings.ToLower(red.Currency), E4ToUSD(red.ServiceFeeE4))
		noteCall(acc.ID, "POST", "/x-direct/orders/confirm", err)
		if err != nil {
			if staleErr(err) {
				return requote(ctx, code, red)
			}
			return holdOrQueue(ctx, "pay", code, red, err)
		}
		if xlogic.QuoteStale(order.ErrorCode) && !order.PaymentAttempted {
			return requote(ctx, code, red)
		}
		if err := applyDirect(code, red, order); err != nil {
			_ = db.SaveXRedemption(*red)
			_ = db.UpdateXCodeStatus(code.ID, code.Status)
			return err
		}
	} else {
		plain := openSeal(code.UpstreamCodeEnc)
		pub, err := client.RedeemCDK(ctx, plain, code.DeviceToken, red.ClientRequestID, strings.ToLower(red.Currency), red.AmountMinor)
		noteCall(acc.ID, "POST", "/public/x-cdk/redeem", err)
		if err != nil {
			return holdOrQueue(ctx, "pay", code, red, err)
		}
		if err := applyPublic(code, red, pub, true); err != nil {
			_ = db.SaveXRedemption(*red)
			_ = db.UpdateXCodeStatus(code.ID, code.Status)
			return err
		}
		if xlogic.ShouldSecondRedeem(code.Channel, code.Status, red.PaymentDispatched) {
			if err := payCDK(ctx, client, code, red, plain); err != nil {
				return err
			}
		}
	}
	_ = db.SaveXRedemption(*red)
	_ = db.UpdateXCodeStatus(code.ID, code.Status)
	return nil
}

func payCDK(ctx context.Context, client *avanfinity.Client, code *db.XCode, red *db.XRedemption, plain string) error {
	if !xlogic.ShouldSecondRedeem(code.Channel, code.Status, red.PaymentDispatched) {
		return nil
	}
	if !gate("redeem", code.AccountID, red, "") {
		return nil
	}
	appendEvent(red, "第二次 redeem：派发付款")
	pub, err := client.RedeemCDK(ctx, plain, code.DeviceToken, red.ClientRequestID, strings.ToLower(red.Currency), red.AmountMinor)
	noteCall(code.AccountID, "POST", "/public/x-cdk/redeem", err)
	if err != nil && staleErr(err) {
		refreshed, perr := client.PreflightCDK(ctx, plain, code.DeviceToken, red.Recipient, red.ClientRequestID)
		noteCall(code.AccountID, "POST", "/public/x-cdk/preflight", perr)
		if perr != nil {
			return holdOrQueue(ctx, "pay", code, red, perr)
		}
		_ = applyPublic(code, red, refreshed, false)
		pub, err = client.RedeemCDK(ctx, plain, code.DeviceToken, red.ClientRequestID, strings.ToLower(red.Currency), red.AmountMinor)
		noteCall(code.AccountID, "POST", "/public/x-cdk/redeem", err)
	}
	if err != nil {
		return holdOrQueue(ctx, "pay", code, red, err)
	}
	red.PaymentDispatched = true
	if err := applyPublic(code, red, pub, true); err != nil {
		_ = db.SaveXRedemption(*red)
		_ = db.UpdateXCodeStatus(code.ID, code.Status)
		return err
	}
	_ = db.SaveXRedemption(*red)
	_ = db.UpdateXCodeStatus(code.ID, code.Status)
	return nil
}

func requote(ctx context.Context, code *db.XCode, red *db.XRedemption) error {
	if moneyMoved(*red) {
		code.Status = "uncertain"
		red.Message = "报价已过期，但付款可能已经发出，只查询不重下单"
		red.ErrorCode = "PAY_UNKNOWN"
		schedule(code, red)
		_ = db.SaveXRedemption(*red)
		_ = db.UpdateXCodeStatus(code.ID, code.Status)
		return nil
	}
	recipient := red.Recipient
	if err := retireQuote(ctx, code, red); err != nil {
		return err
	}
	next, err := ensureRedemption(ctx, *code, recipient)
	if err != nil {
		return err
	}
	*red = next
	if err := runQuote(ctx, code, red, false); err != nil {
		return err
	}
	if code.Status == "quoted" {
		red.Message = "报价已更新，请再确认一次"
		_ = db.SaveXRedemption(*red)
	}
	return nil
}

func staleErr(err error) bool {
	var api *avanfinity.APIError
	return errors.As(err, &api) && xlogic.QuoteStale(api.ErrorCode)
}

func holdOrQueue(ctx context.Context, stage string, code *db.XCode, red *db.XRedemption, err error) error {
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
		appendEvent(red, "报价未成功，稍后重试")
		_ = db.SaveXRedemption(*red)
		return nil
	case xlogic.FailWait:
		red.Message = "开通人数较多，系统会自动继续"
		red.NextPollAt = time.Now().Add(60 * time.Second).UTC().Format("2006-01-02 15:04:05")
		_ = db.SaveXRedemption(*red)
		return nil
	case xlogic.FailBalance:
		code.Status = "funding"
		red.ErrorCode = "SPENDABLE_BALANCE_INSUFFICIENT"
		red.Message = "钱包余额不足，充值后会用原请求继续"
		schedule(code, red)
		_ = db.SaveXRedemption(*red)
		_ = db.UpdateXCodeStatus(code.ID, code.Status)
		return nil
	case xlogic.FailStale:
		return err
	case xlogic.FailUncertain:
		code.Status = "uncertain"
		red.ErrorCode = "PAY_UNKNOWN"
		red.PaymentAttempted = true
		if code.Channel == db.XChannelCDK {
			red.FundingDispatched = true
		}
		red.Message = "付款结果还没确认，卡密已锁定，不会重新支付"
		appendEvent(red, "付款结果不确定，改为只查询")
		schedule(code, red)
		_ = db.SaveXRedemption(*red)
		_ = db.UpdateXCodeStatus(code.ID, code.Status)
		return nil
	default:
		return err
	}
}

func applyPublic(code *db.XCode, red *db.XRedemption, pub *avanfinity.PublicCDK, fromRedeem bool) error {
	if pub == nil {
		return nil
	}
	if pub.AmountMinor > 0 {
		red.AmountMinor = pub.AmountMinor
	}
	if pub.Currency != "" {
		red.Currency = strings.ToLower(pub.Currency)
	}
	if pub.Recipient != "" && red.Recipient != "" && !sameHandle(pub.Recipient, red.Recipient) {
		red.Message = "上游返回的账号和客户填写的不一致，已停止"
		if fromRedeem || red.FundingDispatched {
			code.Status = "uncertain"
			red.PaymentAttempted = true
			red.FundingDispatched = true
			schedule(code, red)
			return errRecipientMismatch
		}
		code.Status = "unused"
		red.FinishedAt = now()
		red.UpstreamStatus = "cancelled"
		return errRecipientMismatch
	}
	if pub.Recipient != "" {
		red.Recipient = NormalizeHandle(pub.Recipient)
	}
	red.CanRetryPreflight = pub.CanRetryPreflight != nil && *pub.CanRetryPreflight
	red.ErrorCode = pub.ErrorCode
	red.Message = pub.Message
	if len(pub.InvoiceURLs) > 0 {
		raw, _ := json.Marshal(pub.InvoiceURLs)
		red.InvoiceURLsEnc = seal(string(raw))
	}
	if fromRedeem {
		var retry *bool
		if pub.CanRetryPreflight != nil {
			retry = pub.CanRetryPreflight
		}
		fund, pay := xlogic.NoteFunding(pub.Status, retry)
		if fund {
			red.FundingDispatched = true
		}
		if pay {
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

func applyDirect(code *db.XCode, red *db.XRedemption, order *avanfinity.DirectOrder) error {
	if order == nil {
		return nil
	}
	if order.ID != "" {
		red.UpstreamOrderID = order.ID
	}
	if order.Recipient != "" && red.Recipient != "" && !sameHandle(order.Recipient, red.Recipient) {
		red.UpstreamOrderID = order.ID
		red.Message = "上游订单的账号和客户填写的不一致，已停止"
		if order.PaymentAttempted {
			red.PaymentAttempted = true
			code.Status = "uncertain"
			schedule(code, red)
		}
		return errRecipientMismatch
	}
	if order.AmountMinor > 0 {
		red.AmountMinor = order.AmountMinor
	}
	if order.Currency != "" {
		red.Currency = strings.ToLower(order.Currency)
	}
	if v, ok := USDToE4(order.EstimatedUSD); ok {
		red.EstimatedUSDE4 = v
	}
	if v, ok := USDToE4(order.ServiceFee); ok {
		red.ServiceFeeE4 = v
	}
	red.PricingVersion = order.PricingVersion
	if order.PaymentAttempted {
		red.PaymentAttempted = true
	}
	red.ErrorCode = order.ErrorCode
	red.Message = order.Message
	if len(order.InvoiceURLs) > 0 {
		raw, _ := json.Marshal(order.InvoiceURLs)
		red.InvoiceURLsEnc = seal(string(raw))
	}
	mapStatus(code, red, order.Status, nil)
	return nil
}

func mapStatus(code *db.XCode, red *db.XRedemption, upstream string, canRetry *bool) {
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

func schedule(code *db.XCode, red *db.XRedemption) {
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

func appendEvent(red *db.XRedemption, text string) {
	var events []map[string]string
	_ = json.Unmarshal([]byte(red.EventsJSON), &events)
	events = append(events, map[string]string{"t": time.Now().Format("01-02 15:04:05"), "s": text})
	raw, _ := json.Marshal(events)
	red.EventsJSON = string(raw)
}

func now() string { return time.Now().UTC().Format("2006-01-02 15:04:05") }

// Result 只凭卡密返回当前进度。
func Result(raw string) (PublicState, error) {
	code, err := db.GetXCodeByCode(strings.TrimSpace(raw))
	if err != nil {
		return PublicState{}, err
	}
	red, rerr := db.LatestXRedemption(code.ID)
	return stateOf(code, red, rerr == nil), nil
}

// PollDue 推进到期的兑换。报价成功不会在这里被确认付款。
func PollDue(ctx context.Context) {
	rows, err := db.DueXRedemptions(20)
	if err != nil || len(rows) == 0 {
		return
	}
	for _, red := range rows {
		if !tryRed(red.ID) {
			continue
		}
		func() {
			defer unlockRed(red.ID)
			code, err := db.GetXCode(red.XCodeID)
			if err != nil {
				return
			}
			red.PollCount++
			switch xlogic.PollAction(code.Status, red.UpstreamStatus, red.UpstreamOrderID) {
			case xlogic.ActionConfirm:
				kind := "confirm"
				if code.Channel == db.XChannelCDK {
					kind = "redeem"
				}
				if !gate(kind, code.AccountID, &red, "queued_confirm") {
					return
				}
				_ = runConfirm(ctx, &code, &red)
			case xlogic.ActionQuote:
				kind := "prepare"
				if code.Channel == db.XChannelCDK {
					kind = "preflight"
				}
				if !gate(kind, code.AccountID, &red, "queued_quote") {
					return
				}
				_ = runQuote(ctx, &code, &red, true)
			case xlogic.ActionRefresh:
				if !gate("result", code.AccountID, &red, "") {
					return
				}
				_ = refresh(ctx, &code, &red)
			default:
				red.NextPollAt = ""
			}
			maybeEscalate(&code, &red)
			_ = db.SaveXRedemption(red)
			_ = db.UpdateXCodeStatus(code.ID, code.Status)
		}()
	}
}

func refresh(ctx context.Context, code *db.XCode, red *db.XRedemption) error {
	acc, err := accountForCode(*code)
	if err != nil {
		return err
	}
	client := clientFor(acc)
	if code.Channel == db.XChannelDirect {
		if red.UpstreamOrderID == "" {
			return nil
		}
		order, err := client.GetDirectOrder(ctx, red.UpstreamOrderID)
		noteCall(acc.ID, "GET", "/x-direct/orders/"+red.UpstreamOrderID, err)
		if err != nil {
			return holdOrQueue(ctx, "read", code, red, err)
		}
		if xlogic.QuoteStale(order.ErrorCode) && !order.PaymentAttempted && !moneyMoved(*red) {
			return requote(ctx, code, red)
		}
		if err := applyDirect(code, red, order); err != nil {
			_ = db.SaveXRedemption(*red)
			_ = db.UpdateXCodeStatus(code.ID, code.Status)
			return err
		}
		_ = db.SaveXRedemption(*red)
		_ = db.UpdateXCodeStatus(code.ID, code.Status)
		return nil
	}
	plain := openSeal(code.UpstreamCodeEnc)
	wasUnknown := red.ErrorCode == "PAY_UNKNOWN"
	pub, err := client.ResultCDK(ctx, plain, code.DeviceToken, red.ClientRequestID)
	noteCall(acc.ID, "POST", "/public/x-cdk/result", err)
	if err != nil {
		return holdOrQueue(ctx, "read", code, red, err)
	}
	if err := applyPublic(code, red, pub, false); err != nil {
		_ = db.SaveXRedemption(*red)
		_ = db.UpdateXCodeStatus(code.ID, code.Status)
		return err
	}
	if wasUnknown && code.Status != "uncertain" && red.ErrorCode == "PAY_UNKNOWN" {
		red.ErrorCode = pub.ErrorCode
	}
	if xlogic.ShouldSecondRedeem(code.Channel, code.Status, red.PaymentDispatched) && red.ErrorCode != "PAY_UNKNOWN" {
		return payCDK(ctx, client, code, red, plain)
	}
	_ = db.SaveXRedemption(*red)
	_ = db.UpdateXCodeStatus(code.ID, code.Status)
	return nil
}

func maybeEscalate(code *db.XCode, red *db.XRedemption) {
	if code.Status == "completed" || code.Status == "unused" || code.Status == "disabled" {
		return
	}
	mins := 120
	if raw, _ := db.GetSetting("x_alert_stuck_minutes"); raw != "" {
		fmt.Sscan(raw, &mins)
	}
	created, err := time.Parse("2006-01-02 15:04:05", red.CreatedAt)
	if err != nil || time.Since(created) < time.Duration(mins)*time.Minute {
		return
	}
	if red.ErrorCode == "STUCK" {
		return
	}
	code.Status = "review_required"
	red.ErrorCode = "STUCK"
	red.Message = "超过处理时限，已转人工"
	notify.SendText("X 会员兑换超时：" + code.Code + " 状态 " + red.UpstreamStatus)
}

// DisableCode 只作废还没开始兑换的码。付款结果未确认的码可能已经动过钱，
// 不能从列表里一键作废，要在兑换记录里人工处理。
func DisableCode(ctx context.Context, id int64) error {
	return disableCode(ctx, id, false)
}

func disableCode(ctx context.Context, id int64, manual bool) error {
	code, err := db.GetXCode(id)
	if err != nil {
		return err
	}
	if code.Status == "completed" {
		return fmt.Errorf("已经开通的码不能作废")
	}
	if !manual && code.Status != "unused" {
		return fmt.Errorf("只有未使用的码可以作废")
	}
	if code.Channel == db.XChannelCDK && code.UpstreamCDKID != "" && code.Status == "unused" {
		acc, err := accountForCode(code)
		if err != nil {
			return err
		}
		err = clientFor(acc).RevokeCDK(ctx, code.UpstreamCDKID, NewUUID())
		noteCall(acc.ID, "POST", "/x-direct/cdks/revoke", err)
		if err != nil {
			return fmt.Errorf("上游撤销失败：%w", err)
		}
	}
	return db.UpdateXCodeStatus(id, "disabled")
}

// Requery 立刻查一次，并写回码的状态。
func Requery(ctx context.Context, redemptionID int64) error {
	red, err := db.GetXRedemption(redemptionID)
	if err != nil {
		return err
	}
	code, err := db.GetXCode(red.XCodeID)
	if err != nil {
		return err
	}
	red.NextPollAt = now()
	if err := refresh(ctx, &code, &red); err != nil && !errors.Is(err, sql.ErrNoRows) {
		_ = db.UpdateXCodeStatus(code.ID, code.Status)
		_ = db.SaveXRedemption(red)
		return err
	}
	if err := db.UpdateXCodeStatus(code.ID, code.Status); err != nil {
		return err
	}
	return db.SaveXRedemption(red)
}

// Resolve 人工处理必须带结果：开通、放回未使用，或作废。只写备注不会让客户一直锁着。
func Resolve(ctx context.Context, id int64, outcome, note string) error {
	note = strings.TrimSpace(note)
	if note == "" {
		return fmt.Errorf("请填写处理备注")
	}
	red, err := db.GetXRedemption(id)
	if err != nil {
		return err
	}
	code, err := db.GetXCode(red.XCodeID)
	if err != nil {
		return err
	}
	stamp := now()
	switch outcome {
	case "completed":
		code.Status = "completed"
		appendEvent(&red, "人工标记已开通："+note)
	case "release":
		if moneyMoved(red) {
			return fmt.Errorf("已经动过钱，不能放回未使用")
		}
		code.Status = "unused"
		appendEvent(&red, "人工放回未使用："+note)
	case "disable":
		if err := disableCode(ctx, code.ID, true); err != nil {
			return err
		}
		code.Status = "disabled"
		appendEvent(&red, "人工作废："+note)
	default:
		return fmt.Errorf("请选择处理结果")
	}
	red.FinishedAt = stamp
	red.ResolvedAt = stamp
	red.ResolvedNote = note
	red.NextPollAt = ""
	if err := db.SaveXRedemption(red); err != nil {
		return err
	}
	return db.UpdateXCodeStatus(code.ID, code.Status)
}
