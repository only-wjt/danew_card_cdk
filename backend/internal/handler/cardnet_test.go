package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
	"github.com/danew/cdk-recharge-system/internal/db"
)

type cardplatformAPIError = cardplatform.APIError

// fakeCardPlatform 模拟主台 OpenAPI：同一个 Idempotency-Key 重放同一批码。
type fakeCardPlatform struct {
	mu        sync.Mutex
	srv       *httptest.Server
	nextID    int64
	batches   map[string][]map[string]any
	issueKeys []string
	// issueMode: ok | pending | reject | short
	issueMode string
	block     chan struct{}
	entered   chan struct{}
	consumed  map[int64]bool
	deleted   map[int64]bool
	regions   []map[string]string
}

func newFakeCardPlatform(t *testing.T) *fakeCardPlatform {
	t.Helper()
	f := &fakeCardPlatform{
		nextID: 1000, batches: map[string][]map[string]any{}, issueMode: "ok",
		consumed: map[int64]bool{}, deleted: map[int64]bool{},
		regions: []map[string]string{{"country": "PH", "currency": "PHP"}, {"country": "US", "currency": "USD"}},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeCardPlatform) write(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (f *fakeCardPlatform) serve(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/openapi/v1")
	switch {
	case r.Method == http.MethodGet && path == "/gpt-direct/plans":
		f.mu.Lock()
		regions := f.regions
		f.mu.Unlock()
		f.write(w, 200, map[string]any{"code": 0, "data": map[string]any{
			"plans": map[string]any{
				"plus":    map[string]any{"label": "Plus", "enabled": true, "serviceFeeUsdMinor": 150},
				"pro_20x": map[string]any{"label": "Pro", "enabled": true, "serviceFeeUsdMinor": 500},
				"go":      map[string]any{"label": "Go", "enabled": false},
			},
			"payment_regions": regions,
		}})
	case r.Method == http.MethodGet && path == "/balance":
		f.write(w, 200, map[string]any{"code": 0, "data": map[string]any{
			"balance": "100.00", "spendable_balance": "80.00", "account_reserve_amount": "20.00"}})
	case r.Method == http.MethodPost && path == "/gpt-direct/cdks":
		f.issue(w, r)
	case strings.HasPrefix(path, "/gpt-direct/cdks/"):
		f.mutate(w, r, strings.TrimPrefix(path, "/gpt-direct/cdks/"))
	default:
		f.write(w, 404, map[string]any{"code": 404, "msg": "not found"})
	}
}

func (f *fakeCardPlatform) issue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Plan           string `json:"plan"`
		Count          int    `json:"count"`
		PaymentCountry string `json:"payment_country"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	key := r.Header.Get("Idempotency-Key")
	f.mu.Lock()
	f.issueKeys = append(f.issueKeys, key)
	mode, block, entered := f.issueMode, f.block, f.entered
	f.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
	}
	if block != nil {
		<-block
	}
	switch mode {
	case "pending":
		f.write(w, 503, map[string]any{"code": 503, "msg": "upstream busy"})
		return
	case "reject":
		f.write(w, 400, map[string]any{"code": 40010, "msg": "余额不足", "error_code": "INSUFFICIENT_BALANCE"})
		return
	}
	f.mu.Lock()
	issued, ok := f.batches[key]
	if !ok {
		for i := 0; i < body.Count; i++ {
			f.nextID++
			issued = append(issued, map[string]any{
				"id": f.nextID, "code": fmt.Sprintf("GPTD-TEST-%06d-%s", f.nextID, strings.ToUpper(body.Plan)),
				"code_prefix": fmt.Sprintf("GPTD-TEST-%06d", f.nextID), "plan": body.Plan, "fee_amount_minor": 150,
			})
		}
		f.batches[key] = issued
	}
	f.mu.Unlock()
	out := issued
	if mode == "short" && len(out) > 1 {
		out = out[:len(out)-1]
	}
	f.write(w, 200, map[string]any{"code": 0, "data": map[string]any{"requested": body.Count, "issued": out}})
}

func (f *fakeCardPlatform) mutate(w http.ResponseWriter, r *http.Request, rest string) {
	var id int64
	fmt.Sscanf(strings.TrimSuffix(rest, "/disable"), "%d", &id)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.consumed[id] {
		f.write(w, 400, map[string]any{"code": 40020, "msg": "CDK 已使用", "error_code": "CDK_CONSUMED"})
		return
	}
	f.deleted[id] = true
	f.write(w, 200, map[string]any{"code": 0, "data": map[string]any{"id": id, "status": "disabled"}})
}

func (f *fakeCardPlatform) setMode(mode string) {
	f.mu.Lock()
	f.issueMode = mode
	f.mu.Unlock()
}

func (f *fakeCardPlatform) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.issueKeys...)
}

type cardnetEnv struct {
	t      *testing.T
	router *gin.Engine
	up     *fakeCardPlatform
}

func newCardnetEnv(t *testing.T) *cardnetEnv {
	t.Helper()
	openHandlerTestDB(t)
	resetCardnetUpstreamCache()
	cardnetLimiter.reset()
	up := newFakeCardPlatform(t)
	gptTestSQL(t, `INSERT INTO card_platform_accounts
		(id,name,site_base,protocol,cred_secret,capabilities,status,is_primary_default,priority)
		VALUES (7,'zovo',?,'spacexcard-legacy','sk_test','openai','active',1,1)`, up.srv.URL)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1/cardnet")
	g.Use(CardnetAuth())
	g.GET("/overview", CardnetOverview)
	g.POST("/cdks/issue", CardnetIssue)
	g.POST("/cdks/revoke", CardnetRevoke)
	r.POST("/admin/cardnet/requests/:id/refetch", AdminRefetchCardnetRequest)
	r.POST("/admin/cardnet/requests/:id/mark-failed", AdminFailCardnetRequest)
	return &cardnetEnv{t: t, router: r, up: up}
}

func (e *cardnetEnv) client(in db.CardnetClientInput) (db.CardnetClient, string) {
	e.t.Helper()
	if in.Name == "" {
		in.Name = "dujiao"
	}
	c, key, err := db.CreateCardnetClient(in)
	if err != nil {
		e.t.Fatalf("create client: %v", err)
	}
	return c, key
}

func (e *cardnetEnv) do(method, path, apiKey, idem, body string) (int, map[string]any) {
	e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func (e *cardnetEnv) issue(apiKey, idem, body string) (int, map[string]any) {
	return e.do(http.MethodPost, "/api/v1/cardnet/cdks/issue", apiKey, idem, body)
}

func issuedCodes(t *testing.T, out map[string]any) []map[string]any {
	t.Helper()
	raw, _ := out["issued"].([]any)
	codes := make([]map[string]any, 0, len(raw))
	for _, it := range raw {
		codes = append(codes, it.(map[string]any))
	}
	return codes
}

func defaultClientInput() db.CardnetClientInput {
	return db.CardnetClientInput{AllowedPlans: []string{"plus", "pro_20x"}, AllowedCountries: []string{"US", "PH"}, DailyCodeLimit: 50}
}

func TestCardnetIssueStoresCodesAndReplays(t *testing.T) {
	e := newCardnetEnv(t)
	client, key := e.client(defaultClientInput())

	body := `{"plan":"plus","count":2,"payment_country":"us","external_order_no":"DJ001","external_ref":"item:1:chunk:0"}`
	st, out := e.issue(key, "dj-DJ001-1-0", body)
	if st != http.StatusOK || out["status"] != "succeeded" || out["replayed"] != false {
		t.Fatalf("issue: %d %v", st, out)
	}
	codes := issuedCodes(t, out)
	if len(codes) != 2 || out["fee_total_minor"].(float64) != 300 {
		t.Fatalf("codes: %v", out)
	}

	// 落库方式与管理端发码一致：legacy 码、记发码账户、不挂代理、带地区与卡网归属。
	for _, it := range codes {
		var kind, source, country, extOrder string
		var account, agent, clientID int64
		err := db.DB.QueryRow(`SELECT code_kind, fulfilled_account_id, COALESCE(assigned_agent_user_id,0),
			issue_source, cardnet_client_id, COALESCE(payment_country,''), external_order_no
			FROM cardplatform_cdk_codes WHERE id = ?`, int64(it["id"].(float64))).
			Scan(&kind, &account, &agent, &source, &clientID, &country, &extOrder)
		if err != nil {
			t.Fatal(err)
		}
		if kind != db.CodeKindLegacy || account != 7 || agent != 0 || source != "cardnet" ||
			clientID != client.ID || country != "US" || extOrder != "DJ001" {
			t.Fatalf("stored row: kind=%s account=%d agent=%d source=%s client=%d country=%s order=%s",
				kind, account, agent, source, clientID, country, extOrder)
		}
		if res := lookupOneCDK(context.Background(), it["code"].(string), "", false); res.Status != "unused" {
			t.Fatalf("lookup %s: %+v", it["code"], res)
		}
		if acc, err := db.CardPlatformAccountForLegacyCode(it["code"].(string)); err != nil || acc.ID != 7 {
			t.Fatalf("redeem route: %+v %v", acc, err)
		}
	}

	// 同键重放：同一批码、不再调卡台。
	st, again := e.issue(key, "dj-DJ001-1-0", body)
	if st != http.StatusOK || again["replayed"] != true {
		t.Fatalf("replay: %d %v", st, again)
	}
	if fmt.Sprint(issuedCodes(t, again)) != fmt.Sprint(codes) {
		t.Fatalf("replay returned different codes")
	}
	if n := len(e.up.keys()); n != 1 {
		t.Fatalf("upstream called %d times", n)
	}

	// 同键不同参：422，不能用 409（卡网会把 409 当成可重试）。
	st, conflict := e.issue(key, "dj-DJ001-1-0", `{"plan":"plus","count":3,"payment_country":"US"}`)
	if st != http.StatusUnprocessableEntity || conflict["error_code"] != "IDEMPOTENCY_KEY_REUSED" {
		t.Fatalf("reuse: %d %v", st, conflict)
	}
}

func TestCardnetAuthAndValidation(t *testing.T) {
	e := newCardnetEnv(t)
	_, key := e.client(db.CardnetClientInput{AllowedPlans: []string{"plus", "pro_50x"}, AllowedCountries: []string{"JP", "US", "PH"}, DailyCodeLimit: 10})
	disabled, disabledKey := e.client(db.CardnetClientInput{Name: "off", AllowedPlans: []string{"plus"}, DailyCodeLimit: 10})
	if err := db.SetCardnetClientStatus(disabled.ID, db.CardnetClientDisabled); err != nil {
		t.Fatal(err)
	}
	_, ipKey := e.client(db.CardnetClientInput{Name: "ip", AllowedPlans: []string{"plus"}, DailyCodeLimit: 10, IPAllowlist: []string{"10.9.9.9"}})

	cases := []struct {
		name, key, idem, body string
		status                int
		code                  string
	}{
		{"no key", "", "k1", `{"plan":"plus","count":1}`, 401, "INVALID_API_KEY"},
		{"agent key", "ak_live_x", "k1", `{"plan":"plus","count":1}`, 401, "INVALID_API_KEY"},
		{"unknown key", "cn_live_nope", "k1", `{"plan":"plus","count":1}`, 401, "INVALID_API_KEY"},
		{"disabled", disabledKey, "k1", `{"plan":"plus","count":1}`, 401, "INVALID_API_KEY"},
		{"ip", ipKey, "k1", `{"plan":"plus","count":1}`, 403, "IP_NOT_ALLOWED"},
		{"missing idem", key, "", `{"plan":"plus","count":1}`, 400, "INVALID_PARAMS"},
		{"bad idem", key, "has space", `{"plan":"plus","count":1}`, 400, "INVALID_PARAMS"},
		{"count", key, "k2", `{"plan":"plus","count":201}`, 400, "INVALID_PARAMS"},
		{"plan not allowed", key, "k3", `{"plan":"pro_20x","count":1}`, 403, "PLAN_NOT_ALLOWED"},
		{"country not allowed", key, "k4", `{"plan":"plus","count":1,"payment_country":"CL"}`, 403, "COUNTRY_NOT_ALLOWED"},
		{"not sellable", key, "k5", `{"plan":"pro_50x","count":1}`, 422, "PLAN_NOT_SELLABLE"},
		{"region unsupported", key, "k6", `{"plan":"plus","count":1,"payment_country":"JP"}`, 422, "COUNTRY_UNSUPPORTED"},
	}
	for _, tc := range cases {
		st, out := e.issue(tc.key, tc.idem, tc.body)
		if st != tc.status || out["error_code"] != tc.code {
			t.Errorf("%s: got %d %v", tc.name, st, out)
		}
	}
	if n := len(e.up.keys()); n != 0 {
		t.Fatalf("rejected requests reached the card platform %d times", n)
	}
}

func TestCardnetUpstreamPendingRetriesWithSameKey(t *testing.T) {
	e := newCardnetEnv(t)
	_, key := e.client(defaultClientInput())
	body := `{"plan":"plus","count":1}`

	e.up.setMode("pending")
	st, out := e.issue(key, "dj-A-1-0", body)
	if st != http.StatusServiceUnavailable || out["error_code"] != "UPSTREAM_PENDING" || out["status"] != "pending" {
		t.Fatalf("pending: %d %v", st, out)
	}
	e.up.setMode("ok")
	st, out = e.issue(key, "dj-A-1-0", body)
	if st != http.StatusOK || len(issuedCodes(t, out)) != 1 {
		t.Fatalf("retry: %d %v", st, out)
	}
	keys := e.up.keys()
	if len(keys) != 2 || keys[0] != keys[1] || !strings.HasPrefix(keys[0], "cn-") {
		t.Fatalf("upstream idempotency keys: %v", keys)
	}
}

func TestCardnetUpstreamRejectedIsTerminal(t *testing.T) {
	e := newCardnetEnv(t)
	_, key := e.client(db.CardnetClientInput{AllowedPlans: []string{"plus"}, DailyCodeLimit: 2})

	e.up.setMode("reject")
	st, out := e.issue(key, "dj-B-1-0", `{"plan":"plus","count":2}`)
	if st != http.StatusUnprocessableEntity || out["error_code"] != "UPSTREAM_REJECTED" || out["status"] != "failed" {
		t.Fatalf("reject: %d %v", st, out)
	}
	if !strings.Contains(fmt.Sprint(out["message"]), "余额不足") {
		t.Fatalf("message should carry upstream text: %v", out)
	}
	// 同键重试只拿到同样的答复，不再打卡台。
	st, _ = e.issue(key, "dj-B-1-0", `{"plan":"plus","count":2}`)
	if st != http.StatusUnprocessableEntity || len(e.up.keys()) != 1 {
		t.Fatalf("retry after reject: %d calls=%d", st, len(e.up.keys()))
	}
	// 失败请求不占额度：换新键能发满额度。
	e.up.setMode("ok")
	if st, out := e.issue(key, "dj-B-1-0-r1", `{"plan":"plus","count":2}`); st != http.StatusOK {
		t.Fatalf("new key after failure: %d %v", st, out)
	}
}

func TestCardnetCountMismatchNeedsReview(t *testing.T) {
	e := newCardnetEnv(t)
	_, key := e.client(defaultClientInput())

	e.up.setMode("short")
	st, out := e.issue(key, "dj-C-1-0", `{"plan":"plus","count":2}`)
	if st != http.StatusBadGateway || out["error_code"] != "UPSTREAM_COUNT_MISMATCH" {
		t.Fatalf("short: %d %v", st, out)
	}
	var stored int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM cardplatform_cdk_codes WHERE issue_source='cardnet'`).Scan(&stored)
	if stored != 0 {
		t.Fatalf("partial batch must not be stored, got %d", stored)
	}
	// 卡网重试不会推进待核对的请求。
	if st, _ := e.issue(key, "dj-C-1-0", `{"plan":"plus","count":2}`); st != http.StatusBadGateway || len(e.up.keys()) != 1 {
		t.Fatalf("client retry on review: %d calls=%d", st, len(e.up.keys()))
	}
	// 管理端重新拉取：卡台重放完整批次后转成功，卡网再调拿到码。
	e.up.setMode("ok")
	req, err := db.GetCardnetRequestByKey(1, "dj-C-1-0")
	if err != nil {
		t.Fatal(err)
	}
	if st, out := e.do(http.MethodPost, fmt.Sprintf("/admin/cardnet/requests/%d/refetch", req.ID), "", "", `{}`); st != http.StatusOK || out["http_status"].(float64) != 200 {
		t.Fatalf("refetch: %d %v", st, out)
	}
	st, out = e.issue(key, "dj-C-1-0", `{"plan":"plus","count":2}`)
	if st != http.StatusOK || len(issuedCodes(t, out)) != 2 || out["replayed"] != true {
		t.Fatalf("after refetch: %d %v", st, out)
	}
}

func TestCardnetMarkFailedAllowsNewKey(t *testing.T) {
	e := newCardnetEnv(t)
	_, key := e.client(defaultClientInput())
	e.up.setMode("short")
	e.issue(key, "dj-D-1-0", `{"plan":"plus","count":2}`)
	req, _ := db.GetCardnetRequestByKey(1, "dj-D-1-0")
	if st, _ := e.do(http.MethodPost, fmt.Sprintf("/admin/cardnet/requests/%d/mark-failed", req.ID), "", "", `{}`); st != http.StatusBadRequest {
		t.Fatalf("note required, got %d", st)
	}
	if st, out := e.do(http.MethodPost, fmt.Sprintf("/admin/cardnet/requests/%d/mark-failed", req.ID), "", "", `{"note":"卡台已手动作废"}`); st != http.StatusOK {
		t.Fatalf("mark failed: %d %v", st, out)
	}
	if st, out := e.issue(key, "dj-D-1-0", `{"plan":"plus","count":2}`); st != http.StatusUnprocessableEntity || out["error_code"] != "MANUAL_FAILED" {
		t.Fatalf("client sees failure: %d %v", st, out)
	}
}

func TestCardnetDailyLimit(t *testing.T) {
	e := newCardnetEnv(t)
	_, key := e.client(db.CardnetClientInput{AllowedPlans: []string{"plus"}, DailyCodeLimit: 3})
	if st, _ := e.issue(key, "q1", `{"plan":"plus","count":2}`); st != http.StatusOK {
		t.Fatalf("first: %d", st)
	}
	st, out := e.issue(key, "q2", `{"plan":"plus","count":2}`)
	if st != http.StatusForbidden || out["error_code"] != "DAILY_LIMIT_EXCEEDED" {
		t.Fatalf("over limit: %d %v", st, out)
	}
	if st, _ := e.issue(key, "q3", `{"plan":"plus","count":1}`); st != http.StatusOK {
		t.Fatalf("within limit: %d", st)
	}
	// 重放已成功的请求不受额度影响。
	if st, _ := e.issue(key, "q1", `{"plan":"plus","count":2}`); st != http.StatusOK {
		t.Fatalf("replay at limit: %d", st)
	}
}

func TestCardnetConcurrentSameKeyOnlyOneUpstreamCall(t *testing.T) {
	e := newCardnetEnv(t)
	_, key := e.client(defaultClientInput())
	e.up.mu.Lock()
	e.up.block = make(chan struct{})
	e.up.entered = make(chan struct{}, 1)
	e.up.mu.Unlock()

	done := make(chan int, 1)
	go func() {
		st, _ := e.issue(key, "dj-E-1-0", `{"plan":"plus","count":1}`)
		done <- st
	}()
	select {
	case <-e.up.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first request never reached the card platform")
	}
	st, out := e.issue(key, "dj-E-1-0", `{"plan":"plus","count":1}`)
	if st != http.StatusConflict || out["error_code"] != "REQUEST_IN_FLIGHT" {
		t.Fatalf("concurrent: %d %v", st, out)
	}
	close(e.up.block)
	if st := <-done; st != http.StatusOK {
		t.Fatalf("first: %d", st)
	}
	if n := len(e.up.keys()); n != 1 {
		t.Fatalf("upstream called %d times", n)
	}
}

func TestCardnetRevoke(t *testing.T) {
	e := newCardnetEnv(t)
	_, key := e.client(defaultClientInput())
	_, otherKey := e.client(db.CardnetClientInput{Name: "other", AllowedPlans: []string{"plus"}, DailyCodeLimit: 10})

	_, out := e.issue(key, "dj-F-1-0", `{"plan":"plus","count":2}`)
	codes := issuedCodes(t, out)
	_, otherOut := e.issue(otherKey, "dj-G-1-0", `{"plan":"plus","count":1}`)
	otherID := int64(issuedCodes(t, otherOut)[0]["id"].(float64))
	freeID := int64(codes[0]["id"].(float64))
	usedID := int64(codes[1]["id"].(float64))

	var usedUpstream int64
	_ = db.DB.QueryRow(`SELECT upstream_id FROM cardplatform_cdk_codes WHERE id=?`, usedID).Scan(&usedUpstream)
	e.up.mu.Lock()
	e.up.consumed[usedUpstream] = true
	e.up.mu.Unlock()

	body := fmt.Sprintf(`{"ids":[%d,%d,%d,999999],"reason":"order_refunded","external_order_no":"DJ-F"}`, freeID, usedID, otherID)
	st, res := e.do(http.MethodPost, "/api/v1/cardnet/cdks/revoke", key, "", body)
	if st != http.StatusOK {
		t.Fatalf("revoke: %d %v", st, res)
	}
	if fmt.Sprint(res["revoked"]) != fmt.Sprintf("[%d]", freeID) {
		t.Fatalf("revoked: %v", res)
	}
	reasons := map[int64]string{}
	for _, f := range res["failed"].([]any) {
		m := f.(map[string]any)
		reasons[int64(m["id"].(float64))] = m["reason"].(string)
	}
	if reasons[usedID] != "CONSUMED" || reasons[otherID] != "NOT_FOUND" || reasons[999999] != "NOT_FOUND" {
		t.Fatalf("failed reasons: %v", reasons)
	}
	if got := lookupOneCDK(context.Background(), codes[0]["code"].(string), "", false); got.Status != "disabled" {
		t.Fatalf("revoked code lookup: %+v", got)
	}
	// 再次作废已作废的码视为成功。
	st, res = e.do(http.MethodPost, "/api/v1/cardnet/cdks/revoke", key, "", fmt.Sprintf(`{"ids":[%d]}`, freeID))
	if st != http.StatusOK || fmt.Sprint(res["revoked"]) != fmt.Sprintf("[%d]", freeID) {
		t.Fatalf("idempotent revoke: %d %v", st, res)
	}
}

func TestCardnetOverview(t *testing.T) {
	e := newCardnetEnv(t)
	_, key := e.client(db.CardnetClientInput{AllowedPlans: []string{"plus", "go"}, AllowedCountries: []string{"US"}, DailyCodeLimit: 9})
	st, out := e.do(http.MethodGet, "/api/v1/cardnet/overview", key, "", "")
	if st != http.StatusOK || out["issuable"] != true {
		t.Fatalf("overview: %d %v", st, out)
	}
	plans := out["plans"].([]any)
	if len(plans) != 1 || plans[0].(map[string]any)["key"] != "plus" {
		t.Fatalf("plans should be sellable ∩ allowed: %v", plans)
	}
	if out["balance"].(map[string]any)["spendable_balance"] != "80.00" {
		t.Fatalf("balance: %v", out["balance"])
	}
	client := out["client"].(map[string]any)
	if client["daily_code_limit"].(float64) != 9 {
		t.Fatalf("client: %v", client)
	}
}

func TestClassifyCardnetUpstream(t *testing.T) {
	cases := []struct {
		err      error
		definite bool
	}{
		{context.DeadlineExceeded, false},
		{&cardplatformAPIError{HTTPStatus: 400, Code: 40010}, true},
		{&cardplatformAPIError{HTTPStatus: 422}, true},
		{&cardplatformAPIError{HTTPStatus: 400, Code: 409}, false},
		{&cardplatformAPIError{HTTPStatus: 409}, false},
		{&cardplatformAPIError{HTTPStatus: 429}, false},
		{&cardplatformAPIError{HTTPStatus: 502}, false},
		{&cardplatformAPIError{HTTPStatus: 401}, false},
		{&cardplatformAPIError{HTTPStatus: 200}, false},
	}
	for _, tc := range cases {
		if _, _, definite := classifyCardnetUpstream(tc.err); definite != tc.definite {
			t.Errorf("%v: definite=%v", tc.err, definite)
		}
	}
}

func TestCardnetDefaultCountrySemantics(t *testing.T) {
	e := newCardnetEnv(t)
	// 未配置授权地区：只允许默认地区 PH，显式写 PH 与不写同一结论。
	_, key := e.client(db.CardnetClientInput{AllowedPlans: []string{"plus"}, DailyCodeLimit: 10})
	if st, out := e.issue(key, "c1", `{"plan":"plus","count":1,"payment_country":"PH"}`); st != http.StatusOK {
		t.Fatalf("explicit PH: %d %v", st, out)
	}
	if st, out := e.issue(key, "c2", `{"plan":"plus","count":1}`); st != http.StatusOK {
		t.Fatalf("implicit PH: %d %v", st, out)
	}
	if st, out := e.issue(key, "c3", `{"plan":"plus","count":1,"payment_country":"US"}`); st != http.StatusForbidden || out["error_code"] != "COUNTRY_NOT_ALLOWED" {
		t.Fatalf("US without grant: %d %v", st, out)
	}
	_, ov := e.do(http.MethodGet, "/api/v1/cardnet/overview", key, "", "")
	if got := fmt.Sprint(ov["client"].(map[string]any)["allowed_countries"]); got != "[PH]" {
		t.Fatalf("overview should expose effective countries, got %s", got)
	}
	// 授权了 US 但没授权 PH：不写地区（=PH）也要拒。
	_, usKey := e.client(db.CardnetClientInput{Name: "us", AllowedPlans: []string{"plus"}, AllowedCountries: []string{"US"}, DailyCodeLimit: 10})
	if st, out := e.issue(usKey, "c4", `{"plan":"plus","count":1}`); st != http.StatusForbidden || out["error_code"] != "COUNTRY_NOT_ALLOWED" {
		t.Fatalf("implicit PH without grant: %d %v", st, out)
	}
}

func TestCardnetReplayAfterPermissionRevoked(t *testing.T) {
	e := newCardnetEnv(t)
	client, key := e.client(defaultClientInput())
	body := `{"plan":"plus","count":1,"payment_country":"US"}`
	st, first := e.issue(key, "r1", body)
	if st != http.StatusOK {
		t.Fatalf("issue: %d %v", st, first)
	}
	if _, err := db.UpdateCardnetClient(client.ID, db.CardnetClientInput{Name: client.Name, AllowedPlans: []string{"pro_20x"}, DailyCodeLimit: 50}); err != nil {
		t.Fatal(err)
	}
	// 已成功的请求照常重放，卡网能拿回已付款的码。
	st, again := e.issue(key, "r1", body)
	if st != http.StatusOK || fmt.Sprint(issuedCodes(t, again)) != fmt.Sprint(issuedCodes(t, first)) {
		t.Fatalf("replay after revoke: %d %v", st, again)
	}
	// 新请求按新的授权把关。
	if st, out := e.issue(key, "r2", body); st != http.StatusForbidden || out["error_code"] != "PLAN_NOT_ALLOWED" {
		t.Fatalf("new request after revoke: %d %v", st, out)
	}
}

func TestAdminCardnetOptions(t *testing.T) {
	e := newCardnetEnv(t)
	e.router.GET("/admin/cardnet/options", AdminCardnetOptions)
	st, out := e.do(http.MethodGet, "/admin/cardnet/options", "", "", "")
	if st != http.StatusOK || out["message"] != nil {
		t.Fatalf("options: %d %v", st, out)
	}
	// 只列可售档位（go 在卡台是停用的）。
	keys := []string{}
	for _, p := range out["plans"].([]any) {
		keys = append(keys, p.(map[string]any)["key"].(string))
	}
	if fmt.Sprint(keys) != "[plus pro_20x]" && fmt.Sprint(keys) != "[pro_20x plus]" {
		t.Fatalf("plans: %v", keys)
	}
	if len(out["payment_regions"].([]any)) != 2 {
		t.Fatalf("regions: %v", out["payment_regions"])
	}

	gptTestSQL(t, `UPDATE card_platform_accounts SET status='disabled' WHERE id=7`)
	resetCardnetUpstreamCache()
	st, out = e.do(http.MethodGet, "/admin/cardnet/options", "", "", "")
	if st != http.StatusOK || out["message"] == nil || len(out["plans"].([]any)) != 0 {
		t.Fatalf("unusable primary should report a message: %d %v", st, out)
	}
}
