package handler

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
	"github.com/danew/cdk-recharge-system/internal/db"
	"github.com/danew/cdk-recharge-system/internal/notify"
	"github.com/danew/cdk-recharge-system/internal/provider"
)

// 卡网（dujiao-next）服务端发码接口：/api/v1/cardnet/*
//
// 卡网付款后用 cn_live_ Key 调本站取码。本站用主台 Key 向卡台批量发码，并按管理端发码
// 同样的方式落库，查单、兑换路由、webhook 回写因此都能覆盖到。
//
// HTTP 状态码是对卡网的契约：4xx（409/429 除外）= 明确拒绝、未发码；409/429/5xx = 结果
// 待定，卡网必须用同一个 Idempotency-Key 重试。改动状态码前先看 docs/cardnet-api-zh.md。

const (
	ctxCardnetClient = "cardnet_client"

	cardnetMaxCount     = cardplatform.MaxIssueCount
	cardnetRevokeMax    = 100
	cardnetDefaultRPM   = 120
	cardnetUpstreamWait = 60 * time.Second
	cardnetClaimLease   = 90 * time.Second
	cardnetOverviewTTL  = 60 * time.Second
	// cardnetSnapshotWait 读卡台套餐/余额的上限：建单前的售卖性校验不能吃掉卡网的请求超时。
	cardnetSnapshotWait   = 8 * time.Second
	cardnetQuotaAlertRate = 0.8
)

var cardnetIdemKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

var cardnetLimiter = newIPLimiter(cardnetDefaultRPM, time.Minute)

func cardnetError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error_code": code, "message": message})
}

// CardnetAuth 只认 cn_live_ Key；停用客户端、白名单外 IP 一律拒绝。
func CardnetAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		parts := strings.SplitN(strings.TrimSpace(c.GetHeader("Authorization")), " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" || !db.IsCardnetKey(parts[1]) {
			cardnetError(c, http.StatusUnauthorized, "INVALID_API_KEY", "缺少或无效的 API Key")
			return
		}
		client, err := db.LookupCardnetClientByKey(parts[1])
		if err != nil {
			if !errors.Is(err, db.ErrCardnetNotFound) {
				log.Printf("[cardnet] lookup client: %v", err)
				cardnetError(c, http.StatusServiceUnavailable, "INTERNAL_ERROR", "鉴权暂不可用，请稍后重试")
				return
			}
			cardnetError(c, http.StatusUnauthorized, "INVALID_API_KEY", "缺少或无效的 API Key")
			return
		}
		if client.Status != db.CardnetClientActive {
			cardnetError(c, http.StatusUnauthorized, "INVALID_API_KEY", "客户端已停用")
			return
		}
		if !client.AllowsIP(c.ClientIP()) {
			cardnetError(c, http.StatusForbidden, "IP_NOT_ALLOWED", "来源 IP 不在白名单内")
			return
		}
		if ok, wait := cardnetLimiter.allow(fmt.Sprintf("cn:%d", client.ID), 1); !ok {
			c.Header("Retry-After", fmt.Sprintf("%d", int(wait.Seconds())+1))
			cardnetError(c, http.StatusTooManyRequests, "RATE_LIMITED", "请求过于频繁，请稍后重试")
			return
		}
		db.TouchCardnetClient(client.ID)
		c.Set(ctxCardnetClient, client)
		c.Next()
	}
}

func cardnetClientFrom(c *gin.Context) db.CardnetClient {
	v, _ := c.Get(ctxCardnetClient)
	client, _ := v.(db.CardnetClient)
	return client
}

// ---- 主台 ----

// cardnetAccountUsable 卡网只发主台单台码；Avanfinity 协议没有批量发码接口。
func cardnetAccountUsable(acc db.CardPlatformAccount) error {
	switch {
	case !strings.EqualFold(acc.Status, "active"):
		return fmt.Errorf("主台已停用")
	case strings.EqualFold(acc.CircuitState, "open"):
		return fmt.Errorf("主台熔断中")
	case strings.TrimSpace(acc.CredSecret) == "":
		return fmt.Errorf("主台未配置 API Key")
	case acc.Protocol == db.AccountProtocolAvanfinity202608 || acc.Protocol == db.AccountProtocolAvanfinityAPIv1:
		return fmt.Errorf("主台协议不支持卡网发码")
	}
	return nil
}

func cardnetPrimaryAccount() (db.CardPlatformAccount, error) {
	acc, err := db.PrimaryCardPlatformAccount()
	if err != nil {
		return acc, err
	}
	return acc, cardnetAccountUsable(acc)
}

// ---- 卡台目录与余额缓存（plans / balance 有频率限制） ----
//
// 每个产品的目录、余额各缓存一份：发 GPT 码只读 GPT 目录，Grok 目录读不到也不影响 GPT 可售。
// 失败结果不缓存：下一次请求立刻重试，而不是把一次抖动放大成一分钟不可售。

type cardnetCatalogSnap struct {
	accountID int64
	plans     *cardplatform.PlansResponse
	at        time.Time
}

type cardnetBalanceSnap struct {
	accountID int64
	balance   *cardplatform.BalanceResponse
	at        time.Time
}

var (
	cardnetSnapMu       sync.Mutex
	cardnetCatalogCache = map[string]*cardnetCatalogSnap{}
	cardnetBalanceCache *cardnetBalanceSnap
)

// cardnetPlans 主台某个产品的档位目录。gpt 不带 product 参数（与原来的请求一致）。
func cardnetPlans(ctx context.Context, acc db.CardPlatformAccount, product string) (*cardplatform.PlansResponse, error) {
	cardnetSnapMu.Lock()
	if s := cardnetCatalogCache[product]; s != nil && s.accountID == acc.ID && time.Since(s.at) < cardnetOverviewTTL {
		cardnetSnapMu.Unlock()
		return s.plans, nil
	}
	cardnetSnapMu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, cardnetSnapshotWait)
	defer cancel()
	cli := cardplatform.NewFromAccount(acc)
	var plans *cardplatform.PlansResponse
	var err error
	if product == cardplatform.CardnetProductGPT {
		plans, err = cli.GetPlans(ctx)
	} else {
		plans, err = cli.GetPlans(ctx, product)
	}
	if err == nil && plans == nil {
		err = errors.New("卡台未返回套餐")
	}
	if err != nil {
		return nil, err
	}
	cardnetSnapMu.Lock()
	cardnetCatalogCache[product] = &cardnetCatalogSnap{accountID: acc.ID, plans: plans, at: time.Now()}
	cardnetSnapMu.Unlock()
	return plans, nil
}

func cardnetBalance(ctx context.Context, acc db.CardPlatformAccount) (*cardplatform.BalanceResponse, error) {
	cardnetSnapMu.Lock()
	if s := cardnetBalanceCache; s != nil && s.accountID == acc.ID && time.Since(s.at) < cardnetOverviewTTL {
		cardnetSnapMu.Unlock()
		return s.balance, nil
	}
	cardnetSnapMu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, cardnetSnapshotWait)
	defer cancel()
	bal, err := cardplatform.NewFromAccount(acc).GetBalance(ctx)
	if err == nil && bal == nil {
		err = errors.New("卡台未返回余额")
	}
	if err != nil {
		return nil, err
	}
	cardnetSnapMu.Lock()
	cardnetBalanceCache = &cardnetBalanceSnap{accountID: acc.ID, balance: bal, at: time.Now()}
	cardnetSnapMu.Unlock()
	return bal, nil
}

func resetCardnetUpstreamCache() {
	cardnetSnapMu.Lock()
	cardnetCatalogCache = map[string]*cardnetCatalogSnap{}
	cardnetBalanceCache = nil
	cardnetSnapMu.Unlock()
}

func regionSupported(plans *cardplatform.PlansResponse, country string) bool {
	if country == "" || plans == nil || len(plans.PaymentRegions) == 0 {
		return true
	}
	for _, r := range plans.PaymentRegions {
		if strings.EqualFold(strings.TrimSpace(r.Country), country) {
			return true
		}
	}
	return false
}

// ---- 可售档位（overview 与管理端选项共用） ----

// cardnetPlanRow 卡网可见的一个档位：Key 是对外键（Grok 带 grok_ 前缀）。
type cardnetPlanRow struct {
	Key     string
	Product string
	Label   string
	Plan    cardplatform.SellablePlan
}

func (r cardnetPlanRow) json() gin.H {
	return gin.H{
		"key": r.Key, "product": r.Product, "label": r.Label, "purchasable": true,
		"service_fee_usd_minor": r.Plan.ServiceFeeUsdMinor, "sort_order": r.Plan.SortOrder,
		"checkout_currency": r.Plan.CheckoutCurrency, "checkout_amount_minor": r.Plan.CheckoutAmountMinor,
	}
}

type cardnetProductState struct {
	Available           bool                         `json:"available"`
	Message             string                       `json:"message,omitempty"`
	PaymentRegions      []cardplatform.PaymentRegion `json:"payment_regions"`
	FixedPaymentCountry string                       `json:"fixed_payment_country,omitempty"`
}

type cardnetCatalogue struct {
	rows     []cardnetPlanRow
	products map[string]cardnetProductState
}

// anyAvailable 至少有一个产品的目录读到了。
func (c cardnetCatalogue) anyAvailable() bool {
	for _, p := range c.products {
		if p.Available {
			return true
		}
	}
	return false
}

// gptRegions 顶层 payment_regions 仍只代表 GPT（老卡网按这个字段选地区）。
func (c cardnetCatalogue) gptRegions() []cardplatform.PaymentRegion {
	return c.products[cardplatform.CardnetProductGPT].PaymentRegions
}

// loadCardnetCatalogue 并发读 GPT、Grok 两个目录，排成先 GPT 后 Grok。永远不含 X。
func loadCardnetCatalogue(ctx context.Context, acc db.CardPlatformAccount) cardnetCatalogue {
	grokSupported := provider.SupportsPaymentCountry(acc.Protocol)
	var gptPlans, grokPlans *cardplatform.PlansResponse
	var gptErr, grokErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		gptPlans, gptErr = cardnetPlans(ctx, acc, cardplatform.CardnetProductGPT)
	}()
	if grokSupported {
		wg.Add(1)
		go func() {
			defer wg.Done()
			grokPlans, grokErr = cardnetPlans(ctx, acc, cardplatform.CardnetProductGrok)
		}()
	}
	wg.Wait()

	out := cardnetCatalogue{products: map[string]cardnetProductState{}}
	gpt := cardnetProductState{PaymentRegions: []cardplatform.PaymentRegion{}}
	if gptErr != nil {
		gpt.Message = "卡台 ChatGPT 套餐暂不可读"
	} else {
		gpt.Available = true
		if gptPlans.PaymentRegions != nil {
			gpt.PaymentRegions = gptPlans.PaymentRegions
		}
		for _, p := range gptPlans.SellablePlans() {
			// GPT 目录里混进 X / Grok 形状的键（老卡台无注册表时会透传整张定价表）一律不列。
			key, product, err := cardplatform.CardnetCanonicalPlan(p.Key)
			if err != nil || product != cardplatform.CardnetProductGPT {
				continue
			}
			out.rows = append(out.rows, cardnetPlanRow{Key: key, Product: product, Label: p.Label, Plan: p})
		}
	}
	out.products[cardplatform.CardnetProductGPT] = gpt

	grok := cardnetProductState{PaymentRegions: []cardplatform.PaymentRegion{}, FixedPaymentCountry: cardplatform.CardnetGrokCountry}
	switch {
	case !grokSupported:
		grok.Message = "当前主台不支持 Grok 订阅码"
	case grokErr != nil:
		grok.Message = "卡台 Grok 套餐暂不可读"
	default:
		grok.Available = true
		if grokPlans.PaymentRegions != nil {
			grok.PaymentRegions = grokPlans.PaymentRegions
		}
		for _, p := range grokPlans.SellablePlans() {
			// 注册表是裸键；对外一律带 grok_ 前缀。
			key := cardplatform.GrokIssuePlan(p.Key)
			if !cardplatform.IsGrokPlan(key) {
				continue
			}
			out.rows = append(out.rows, cardnetPlanRow{
				Key: key, Product: cardplatform.CardnetProductGrok,
				Label: nonEmptyStr(cardplatform.GrokPlanLabel(key), p.Label), Plan: p,
			})
		}
	}
	out.products[cardplatform.CardnetProductGrok] = grok
	return out
}

// CardnetOverview GET /api/v1/cardnet/overview
func CardnetOverview(c *gin.Context) {
	client := cardnetClientFrom(c)
	issuedToday, _ := db.CardnetIssuedToday(client.ID)
	out := gin.H{
		"issuable":        false,
		"plans":           []gin.H{},
		"payment_regions": []cardplatform.PaymentRegion{},
		"products":        gin.H{},
		"client": gin.H{
			"allowed_plans":     client.AllowedPlans,
			"allowed_countries": client.EffectiveCountries(),
			"daily_code_limit":  client.DailyCodeLimit,
			"issued_today":      issuedToday,
		},
		"checked_at": time.Now().UTC().Format(time.RFC3339),
	}
	acc, accErr := db.PrimaryCardPlatformAccount()
	if accErr != nil {
		out["message"] = "未配置卡台账户"
		c.JSON(http.StatusOK, out)
		return
	}
	out["platform"] = gin.H{
		"account_id": acc.ID, "name": acc.Name, "status": acc.Status, "circuit": acc.CircuitState,
		"supports_payment_country": true,
	}
	if err := cardnetAccountUsable(acc); err != nil {
		out["message"] = err.Error()
		c.JSON(http.StatusOK, out)
		return
	}
	var bal *cardplatform.BalanceResponse
	var balErr error
	balDone := make(chan struct{})
	go func() {
		defer close(balDone)
		bal, balErr = cardnetBalance(c.Request.Context(), acc)
	}()
	cat := loadCardnetCatalogue(c.Request.Context(), acc)
	<-balDone
	if balErr == nil {
		out["balance"] = gin.H{
			"balance":                bal.Balance.String(),
			"spendable_balance":      bal.SpendableBalance.String(),
			"account_reserve_amount": bal.AccountReserveAmount.String(),
		}
	}
	out["products"] = cat.products
	out["payment_regions"] = cat.gptRegions()
	if !cat.anyAvailable() {
		out["message"] = "卡台套餐暂不可读"
		c.JSON(http.StatusOK, out)
		return
	}
	plans := []gin.H{}
	for _, r := range cat.rows {
		if client.AllowsPlan(r.Key) {
			plans = append(plans, r.json())
		}
	}
	out["plans"] = plans
	out["issuable"] = balErr == nil
	c.JSON(http.StatusOK, out)
}

// ---- 发码 ----

type cardnetIssueBody struct {
	Plan            string `json:"plan"`
	Count           int    `json:"count"`
	PaymentCountry  string `json:"payment_country"`
	ExternalOrderNo string `json:"external_order_no"`
	ExternalRef     string `json:"external_ref"`
}

// CardnetIssue POST /api/v1/cardnet/cdks/issue
func CardnetIssue(c *gin.Context) {
	client := cardnetClientFrom(c)
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if !cardnetIdemKeyPattern.MatchString(key) {
		cardnetError(c, http.StatusBadRequest, "INVALID_PARAMS", "Idempotency-Key 必填，1–128 位字母、数字或 ._:-")
		return
	}
	var body cardnetIssueBody
	if err := c.ShouldBindJSON(&body); err != nil {
		cardnetError(c, http.StatusBadRequest, "INVALID_PARAMS", "请求体不是有效 JSON")
		return
	}
	country := strings.ToUpper(strings.TrimSpace(body.PaymentCountry))
	switch {
	case strings.TrimSpace(body.Plan) == "":
		cardnetError(c, http.StatusBadRequest, "INVALID_PARAMS", "plan 必填")
		return
	case body.Count < 1 || body.Count > cardnetMaxCount:
		cardnetError(c, http.StatusBadRequest, "INVALID_PARAMS", fmt.Sprintf("count 须在 1–%d 之间", cardnetMaxCount))
		return
	case country != "" && len(country) != 2:
		cardnetError(c, http.StatusBadRequest, "INVALID_PARAMS", "payment_country 应为两位国家代码")
		return
	case len(body.ExternalOrderNo) > 64 || len(body.ExternalRef) > 128:
		cardnetError(c, http.StatusBadRequest, "INVALID_PARAMS", "external_order_no / external_ref 过长")
		return
	}
	// 授权、幂等哈希、落库都用规范化后的键与地区：GPT 规范化前后不变，存量请求重放不受影响。
	plan, product, perr := cardplatform.CardnetCanonicalPlan(body.Plan)
	if perr != nil {
		cardnetError(c, http.StatusUnprocessableEntity, "PLAN_NOT_SELLABLE", perr.Error())
		return
	}
	if product == cardplatform.CardnetProductGrok {
		// Grok 是美元固定价：只认 US，空按 US 计。传别的地区明确拒绝，不能静默改掉买家看到的承诺。
		if country != "" && country != cardplatform.CardnetGrokCountry {
			cardnetError(c, http.StatusUnprocessableEntity, "COUNTRY_UNSUPPORTED", "Grok 只支持 US 付款，收到 "+country)
			return
		}
		country = cardplatform.CardnetGrokCountry
	}
	req, err := db.GetCardnetRequestByKey(client.ID, key)
	switch {
	case err == nil:
		if req.ParamsHash != db.CardnetParamsHash(plan, country, body.Count) {
			cardnetError(c, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED", "同一个 Idempotency-Key 已用于不同参数的请求")
			return
		}
		// 已有结论的请求原样答复：授权范围事后收紧，也不能让卡网拿不回已发出的码。
		// 只有还要调卡台的 pending 请求才按当前授权重新把关。
		if req.Status == db.CardnetRequestPending && !cardnetPermitted(c, client, plan, product, country) {
			return
		}
	case errors.Is(err, db.ErrCardnetNotFound):
		if !cardnetPermitted(c, client, plan, product, country) {
			return
		}
		created, ok := createCardnetRequest(c, client, key, plan, product, country, body)
		if !ok {
			return
		}
		req = created
	default:
		log.Printf("[cardnet] load request: %v", err)
		cardnetError(c, http.StatusServiceUnavailable, "INTERNAL_ERROR", "读取请求失败，请用同一个键重试")
		return
	}
	status, out := runCardnetIssue(c.Request.Context(), req, false)
	c.JSON(status, out)
}

// cardnetPermitted 校验客户端授权范围；不通过时已写响应。
// 允许地区只约束 GPT：Grok 只有 US 一个地区，再约束一次只会造成「没勾 US 就卖不了 Grok」。
func cardnetPermitted(c *gin.Context, client db.CardnetClient, plan, product, country string) bool {
	if !client.AllowsPlan(plan) {
		cardnetError(c, http.StatusForbidden, "PLAN_NOT_ALLOWED", "该客户端未被授权发放套餐 "+plan)
		return false
	}
	if product == cardplatform.CardnetProductGPT && !client.AllowsCountry(country) {
		shown := country
		if shown == "" {
			shown = db.CardnetDefaultCountry + "（默认）"
		}
		cardnetError(c, http.StatusForbidden, "COUNTRY_NOT_ALLOWED", "该客户端未被授权付款地区 "+shown)
		return false
	}
	return true
}

// createCardnetRequest 新键：做售卖性校验后幂等建单。ok=false 时已写响应。
func createCardnetRequest(c *gin.Context, client db.CardnetClient, key, plan, product, country string, body cardnetIssueBody) (db.CardnetIssueRequest, bool) {
	acc, err := cardnetPrimaryAccount()
	if err != nil {
		cardnetError(c, http.StatusServiceUnavailable, "ISSUING_UNAVAILABLE", "暂无可用卡台："+err.Error())
		return db.CardnetIssueRequest{}, false
	}
	grok := product == cardplatform.CardnetProductGrok
	if grok && !provider.SupportsPaymentCountry(acc.Protocol) {
		cardnetError(c, http.StatusUnprocessableEntity, "PLAN_NOT_SELLABLE", "当前主台不支持 Grok 订阅码")
		return db.CardnetIssueRequest{}, false
	}
	// 拿不到档位表时不拦，交给卡台发码时校验，避免一次抖动就让卡网发不了码。
	if plans, perr := cardnetPlans(c.Request.Context(), acc, product); perr == nil {
		// Grok 目录的键是裸键；目录读到了却是空的，说明卡台没开 Grok（老卡台忽略 product 参数），不能放行。
		lookup := plan
		if grok {
			lookup = cardplatform.GrokBarePlan(plan)
		}
		if sellable := plans.SellableKeys(); (grok || len(sellable) > 0) && !sellable[lookup] {
			cardnetError(c, http.StatusUnprocessableEntity, "PLAN_NOT_SELLABLE", "卡台未开放档位 "+plan)
			return db.CardnetIssueRequest{}, false
		}
		// Grok 地区已在入口收成 US（卡台 Grok 只按 US 收单），不再对 GPT 地区表校验。
		if !grok && !regionSupported(plans, country) {
			cardnetError(c, http.StatusUnprocessableEntity, "COUNTRY_UNSUPPORTED", "卡台不支持付款地区 "+country)
			return db.CardnetIssueRequest{}, false
		}
	}
	req, created, err := db.CreateOrGetCardnetRequest(db.CardnetIssueParams{
		ClientID: client.ID, IdempotencyKey: key, Plan: plan, PaymentCountry: country, Count: body.Count,
		AccountID: acc.ID, ExternalOrderNo: body.ExternalOrderNo, ExternalRef: body.ExternalRef,
		DailyCodeLimit: client.DailyCodeLimit,
	})
	if errors.Is(err, db.ErrCardnetQuotaExceeded) {
		cardnetError(c, http.StatusForbidden, "DAILY_LIMIT_EXCEEDED", fmt.Sprintf("已超过每日发码额度 %d 张", client.DailyCodeLimit))
		return db.CardnetIssueRequest{}, false
	}
	if err != nil {
		log.Printf("[cardnet] create request: %v", err)
		cardnetError(c, http.StatusServiceUnavailable, "INTERNAL_ERROR", "建单失败，请用同一个键重试")
		return db.CardnetIssueRequest{}, false
	}
	if !created && req.ParamsHash != db.CardnetParamsHash(plan, country, body.Count) {
		// 并发的同键请求抢先建了单，参数却不同。
		cardnetError(c, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED", "同一个 Idempotency-Key 已用于不同参数的请求")
		return db.CardnetIssueRequest{}, false
	}
	if created {
		maybeAlertCardnetQuota(client)
	}
	return req, true
}

// runCardnetIssue 推进一个请求到能回答卡网的状态。管理端「重新拉取」复用同一路径。
func runCardnetIssue(ctx context.Context, req db.CardnetIssueRequest, allowReview bool) (int, gin.H) {
	switch req.Status {
	case db.CardnetRequestSucceeded:
		return cardnetSucceededResponse(req, true)
	case db.CardnetRequestFailed:
		return http.StatusUnprocessableEntity, cardnetErrBody(req, nonEmptyStr(req.ErrorCode, "UPSTREAM_REJECTED"), req.ErrorMessage)
	case db.CardnetRequestReview:
		// 待人工核对的请求只能由管理端「重新拉取」推进，卡网重试只拿到同样的答复。
		if !allowReview {
			return http.StatusBadGateway, cardnetErrBody(req, nonEmptyStr(req.ErrorCode, "UPSTREAM_COUNT_MISMATCH"), req.ErrorMessage)
		}
	}
	claimed, err := db.ClaimCardnetRequest(req.ID, cardnetClaimLease)
	if err != nil {
		log.Printf("[cardnet] claim %s: %v", req.RequestID, err)
		return http.StatusServiceUnavailable, cardnetErrBody(req, "INTERNAL_ERROR", "占用请求失败，请用同一个键重试")
	}
	if !claimed {
		return http.StatusConflict, cardnetErrBody(req, "REQUEST_IN_FLIGHT", "该请求正在处理中，请稍后用同一个键重试")
	}

	acc, err := db.GetCardPlatformAccount(req.AccountID)
	if err == nil {
		err = cardnetAccountUsable(acc)
	}
	if err != nil {
		_ = db.ReleaseCardnetRequest(req.ID, "ISSUING_UNAVAILABLE", err.Error())
		return http.StatusServiceUnavailable, cardnetErrBody(req, "ISSUING_UNAVAILABLE", "发码卡台暂不可用："+err.Error())
	}
	pref, hasPref := issuePrefFromAccount(acc.ID)
	pref.PaymentCountry = req.PaymentCountry
	prefs := []cardplatform.IssueCardPref{}
	if hasPref || req.PaymentCountry != "" {
		prefs = append(prefs, pref)
	}

	// 脱离调用方连接：卡网断开不能打断一笔可能已扣费的发码。
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cardnetUpstreamWait)
	defer cancel()
	// Grok 发给卡台的键显式带 grok_ 前缀（库里存的已是这个写法，再包一层只为防御）：
	// 裸键要靠卡台反查产品，premium_* 会落到 X。
	grok := cardplatform.CardnetPlanProduct(req.Plan) == cardplatform.CardnetProductGrok
	wirePlan := req.Plan
	if grok {
		wirePlan = cardplatform.GrokIssuePlan(req.Plan)
	}
	res, err := cardplatform.NewFromAccount(acc).IssueCDKs(callCtx, wirePlan, req.Count, req.UpstreamIdemKey, prefs...)
	if err != nil {
		code, msg, definite := classifyCardnetUpstream(err)
		if definite {
			if ok, ferr := db.FailCardnetRequest(req.ID, code, msg); ferr != nil || !ok {
				return reloadCardnetResponse(req)
			}
			req.Status = db.CardnetRequestFailed
			log.Printf("[cardnet] %s rejected by card platform: %s", req.RequestID, msg)
			return http.StatusUnprocessableEntity, cardnetErrBody(req, code, msg)
		}
		_ = db.ReleaseCardnetRequest(req.ID, code, msg)
		log.Printf("[cardnet] %s upstream pending: %s", req.RequestID, msg)
		return http.StatusServiceUnavailable, cardnetErrBody(req, code, "结果待定，请用同一个键重试："+msg)
	}

	codes := make([]db.CardnetCodeToSave, 0, len(res.Issued))
	mismatch := ""
	for _, it := range res.Issued {
		code := strings.TrimSpace(it.Code)
		if code == "" || it.ID <= 0 {
			continue
		}
		plan := it.Plan
		if grok {
			// 卡台回的 Grok 档位是裸键，裸 premium_* 与 X 同名：落库补成 grok_ 前缀，
			// 查单、通知、统计才不会显示成 X。档位对不上（卡台按别的档发了）不能交给买家。
			plan = cardplatform.GrokIssuePlan(firstNonEmpty(it.Plan, req.Plan))
			if plan != req.Plan && mismatch == "" {
				mismatch = fmt.Sprintf("卡台返回档位 %s，请求的是 %s", strings.TrimSpace(it.Plan), req.Plan)
			}
		}
		codes = append(codes, db.CardnetCodeToSave{
			UpstreamID: it.ID, Code: code, Prefix: it.CodePrefix, Plan: plan, FeeAmountMinor: it.FeeAmountMinor,
		})
	}
	if len(codes) != req.Count || len(res.Issued) != req.Count {
		msg := fmt.Sprintf("卡台返回 %d 张完整码（共 %d 条），期望 %d 张", len(codes), len(res.Issued), req.Count)
		return cardnetNeedsReview(req, "UPSTREAM_COUNT_MISMATCH", msg)
	}
	if mismatch != "" {
		return cardnetNeedsReview(req, "UPSTREAM_PLAN_MISMATCH", mismatch)
	}
	switch err := db.CompleteCardnetRequest(req, codes); {
	case err == nil:
		_ = db.MarkCardPlatformAccountOK(acc.ID)
		fresh, gerr := db.GetCardnetRequest(req.ID)
		if gerr != nil {
			return http.StatusServiceUnavailable, cardnetErrBody(req, "INTERNAL_ERROR", "已发码，读取结果失败，请用同一个键重试")
		}
		return cardnetSucceededResponse(fresh, false)
	case errors.Is(err, db.ErrCardnetCodeConflict):
		return cardnetNeedsReview(req, "CODE_CONFLICT", "卡台返回的码已属于另一个卡网请求")
	case errors.Is(err, db.ErrCardnetNotPending):
		return reloadCardnetResponse(req)
	default:
		// 卡台已发码但本站没写进去：保持 pending，下次同键会让卡台重放同一批码再落库。
		log.Printf("[cardnet] %s save codes: %v", req.RequestID, err)
		_ = db.ReleaseCardnetRequest(req.ID, "SAVE_FAILED", err.Error())
		return http.StatusServiceUnavailable, cardnetErrBody(req, "UPSTREAM_PENDING", "已发码但保存失败，请用同一个键重试")
	}
}

// cardnetNeedsReview 卡台结果异常：请求转待人工核对并告警，卡网同键重试只得到同样的 502。
func cardnetNeedsReview(req db.CardnetIssueRequest, code, msg string) (int, gin.H) {
	_, _ = db.MarkCardnetRequestReview(req.ID, code, msg)
	req.Status = db.CardnetRequestReview
	alertCardnet(fmt.Sprintf("卡网发码待人工核对\n请求：%s\n卡网订单：%s\n档位：%s × %d\n%s",
		req.RequestID, nonEmptyStr(req.ExternalOrderNo, "—"), gptPlanLabel(req.Plan), req.Count, msg))
	return http.StatusBadGateway, cardnetErrBody(req, code, msg)
}

func reloadCardnetResponse(req db.CardnetIssueRequest) (int, gin.H) {
	fresh, err := db.GetCardnetRequest(req.ID)
	if err != nil {
		return http.StatusServiceUnavailable, cardnetErrBody(req, "INTERNAL_ERROR", "读取请求失败，请用同一个键重试")
	}
	switch fresh.Status {
	case db.CardnetRequestSucceeded:
		return cardnetSucceededResponse(fresh, true)
	case db.CardnetRequestFailed:
		return http.StatusUnprocessableEntity, cardnetErrBody(fresh, nonEmptyStr(fresh.ErrorCode, "UPSTREAM_REJECTED"), fresh.ErrorMessage)
	case db.CardnetRequestReview:
		return http.StatusBadGateway, cardnetErrBody(fresh, nonEmptyStr(fresh.ErrorCode, "UPSTREAM_COUNT_MISMATCH"), fresh.ErrorMessage)
	}
	return http.StatusConflict, cardnetErrBody(fresh, "REQUEST_IN_FLIGHT", "该请求正在处理中，请稍后用同一个键重试")
}

// classifyCardnetUpstream 区分「卡台明确拒绝、没发码」与「结果待定」。
// 拿不准时一律按待定处理：误判成待定只是多重试几次，误判成失败会让卡网换键重买。
func classifyCardnetUpstream(err error) (code, msg string, definite bool) {
	var ae *cardplatform.APIError
	if !errors.As(err, &ae) {
		return "UPSTREAM_PENDING", err.Error(), false
	}
	msg = strings.TrimSpace(ae.Msg)
	if ae.ErrorCode != "" {
		msg = strings.TrimSpace(ae.ErrorCode + " " + msg)
	}
	// 卡台有时在 HTTP 2xx 里用业务码表达冲突/限流，客户端会把 HTTP 状态归一成 400，
	// 这里把业务码也看一遍，免得把「处理中」误判成拒绝。
	if bc := ae.Code; bc == http.StatusConflict || bc == http.StatusTooManyRequests || (bc >= 500 && bc < 600) {
		return "UPSTREAM_PENDING", msg, false
	}
	switch st := ae.HTTPStatus; {
	case st == http.StatusUnauthorized || st == http.StatusForbidden:
		// 本站主台 Key 的问题，不是这笔请求的问题：修好 Key 后同键重试即可。
		return "ISSUING_UNAVAILABLE", msg, false
	case st == http.StatusRequestTimeout || st == http.StatusConflict || st == http.StatusTooEarly ||
		st == http.StatusTooManyRequests || st >= 500 || st < 400:
		return "UPSTREAM_PENDING", msg, false
	default:
		return "UPSTREAM_REJECTED", msg, true
	}
}

func cardnetErrBody(req db.CardnetIssueRequest, code, msg string) gin.H {
	return gin.H{
		"error_code": code, "message": msg,
		"request_id": req.RequestID, "status": req.Status,
	}
}

func cardnetSucceededResponse(req db.CardnetIssueRequest, replayed bool) (int, gin.H) {
	codes, err := db.ListCardnetRequestCodes(req.ID)
	if err != nil {
		return http.StatusServiceUnavailable, cardnetErrBody(req, "INTERNAL_ERROR", "读取码失败，请用同一个键重试")
	}
	if len(codes) != req.Count {
		// 成功请求的码数对不上只可能是数据被改动过，宁可拒绝也不少交付。
		return http.StatusBadGateway, cardnetErrBody(req, "UPSTREAM_COUNT_MISMATCH",
			fmt.Sprintf("本站记录 %d 张码，期望 %d 张", len(codes), req.Count))
	}
	issued := make([]gin.H, 0, len(codes))
	for _, it := range codes {
		issued = append(issued, gin.H{
			"id": it.ID, "code": it.Code, "plan": it.Plan,
			"payment_country": it.PaymentCountry, "fee_amount_minor": it.FeeAmountMinor,
		})
	}
	return http.StatusOK, gin.H{
		"request_id":      req.RequestID,
		"status":          db.CardnetRequestSucceeded,
		"plan":            req.Plan,
		"payment_country": req.PaymentCountry,
		"count":           req.Count,
		"issued":          issued,
		"fee_total_minor": req.FeeTotalMinor,
		"replayed":        replayed,
	}
}

// ---- 作废 ----

// CardnetRevoke POST /api/v1/cardnet/cdks/revoke
func CardnetRevoke(c *gin.Context) {
	client := cardnetClientFrom(c)
	var body struct {
		IDs             []int64 `json:"ids"`
		Reason          string  `json:"reason"`
		ExternalOrderNo string  `json:"external_order_no"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.IDs) == 0 {
		cardnetError(c, http.StatusBadRequest, "INVALID_PARAMS", "ids 必填")
		return
	}
	if len(body.IDs) > cardnetRevokeMax {
		cardnetError(c, http.StatusBadRequest, "INVALID_PARAMS", fmt.Sprintf("ids 每次最多 %d 个", cardnetRevokeMax))
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 2*time.Minute)
	defer cancel()
	revoked := []int64{}
	failed := []gin.H{}
	seen := map[int64]bool{}
	for _, id := range body.IDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		reason, msg := revokeCardnetCode(ctx, client, id)
		if reason == "" {
			revoked = append(revoked, id)
			continue
		}
		failed = append(failed, gin.H{"id": id, "reason": reason, "message": msg})
	}
	db.WriteAudit("cardnet:"+client.Name, "cardnet_revoke",
		fmt.Sprintf("order=%s reason=%s revoked=%d failed=%d", body.ExternalOrderNo, body.Reason, len(revoked), len(failed)), c.ClientIP())
	if len(failed) > 0 {
		log.Printf("[cardnet] revoke order=%s failed=%v", body.ExternalOrderNo, failed)
	}
	c.JSON(http.StatusOK, gin.H{"revoked": revoked, "failed": failed})
}

// revokeCardnetCode 删除退费优先，失败再停用。返回空 reason 表示已作废。
func revokeCardnetCode(ctx context.Context, client db.CardnetClient, id int64) (reason, msg string) {
	code, err := db.GetCardnetCodeForClient(id, client.ID)
	if errors.Is(err, db.ErrCardnetNotFound) {
		return "NOT_FOUND", "不存在或不属于该客户端"
	}
	if err != nil {
		return "UPSTREAM_ERROR", "读取失败"
	}
	switch strings.ToLower(code.Status) {
	case "disabled":
		return "", ""
	case "consumed", "used", "redeemed":
		return "CONSUMED", "卡密已兑换"
	case "reserved":
		return "RESERVED", "卡密兑换进行中"
	}
	if code.UpstreamID <= 0 || code.AccountID <= 0 {
		return "UPSTREAM_ERROR", "缺少卡台记录"
	}
	acc, err := db.GetCardPlatformAccount(code.AccountID)
	if err != nil {
		return "UPSTREAM_ERROR", "发码卡台不存在"
	}
	cli := cardplatform.NewFromAccount(acc)
	delErr := cli.DeleteCDK(ctx, code.UpstreamID)
	if delErr == nil {
		_ = db.UpdateCardplatformCDKStatusByRowID(code.ID, "disabled")
		return "", ""
	}
	disErr := cli.DisableCDK(ctx, code.UpstreamID)
	if disErr == nil {
		_ = db.UpdateCardplatformCDKStatusByRowID(code.ID, "disabled")
		return "", ""
	}
	return classifyRevokeError(disErr, delErr)
}

func classifyRevokeError(errs ...error) (reason, msg string) {
	for _, err := range errs {
		if err == nil {
			continue
		}
		text := err.Error()
		var ae *cardplatform.APIError
		if errors.As(err, &ae) {
			text = strings.TrimSpace(ae.ErrorCode + " " + ae.Msg)
		}
		lower := strings.ToLower(text)
		switch {
		case strings.Contains(lower, "consum") || strings.Contains(lower, "used") ||
			strings.Contains(text, "已使用") || strings.Contains(text, "已消耗") || strings.Contains(text, "已兑换"):
			return "CONSUMED", text
		case strings.Contains(lower, "reserv") || strings.Contains(text, "占用") || strings.Contains(text, "进行中"):
			return "RESERVED", text
		}
		msg = text
	}
	return "UPSTREAM_ERROR", msg
}

// ---- 告警 ----

var cardnetQuotaAlerted sync.Map // "clientID|YYYY-MM-DD" → true

func maybeAlertCardnetQuota(client db.CardnetClient) {
	if client.DailyCodeLimit <= 0 {
		return
	}
	used, err := db.CardnetIssuedToday(client.ID)
	if err != nil || float64(used) < float64(client.DailyCodeLimit)*cardnetQuotaAlertRate {
		return
	}
	key := fmt.Sprintf("%d|%s", client.ID, db.CardnetDayStartUTC(time.Now()))
	if _, loaded := cardnetQuotaAlerted.LoadOrStore(key, true); loaded {
		return
	}
	alertCardnet(fmt.Sprintf("卡网客户端「%s」今日已发 %d / %d 张，接近每日额度", client.Name, used, client.DailyCodeLimit))
}

func alertCardnet(text string) {
	notify.SendText("⚠️ " + html.EscapeString(text))
}

func nonEmptyStr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
