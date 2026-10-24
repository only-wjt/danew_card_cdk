package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 卡网（dujiao-next）服务端发码。卡网付款后调本站取码，码按管理端发码同样的方式落库，
// 查单、兑换路由、webhook 回写因此都能覆盖到。设计见 docs/卡网发码接口设计-2026-10-17.md。

const (
	CardnetClientActive   = "active"
	CardnetClientDisabled = "disabled"

	CardnetRequestPending   = "pending"
	CardnetRequestSucceeded = "succeeded"
	CardnetRequestFailed    = "failed"
	// CardnetRequestReview 卡台返回张数与请求不符等异常：不交付，等人工核对。
	CardnetRequestReview = "review"

	// CardnetIssueSource 写在 cardplatform_cdk_codes.issue_source。
	CardnetIssueSource = "cardnet"

	cardnetKeyPrefix = "cn_live_"
)

var (
	ErrCardnetQuotaExceeded = errors.New("cardnet daily limit exceeded")
	ErrCardnetNotFound      = errors.New("cardnet record not found")
	// ErrCardnetCodeConflict 卡台返回的码已属于另一个卡网请求，不能再交给第二个订单。
	ErrCardnetCodeConflict = errors.New("code already belongs to another cardnet request")
	// ErrCardnetNotPending 请求已不在可完成状态（被并发调用或人工处理抢先）。
	ErrCardnetNotPending = errors.New("cardnet request is no longer pending")
)

// cardnetDayLocation 每日额度按北京时间自然日计算。
var cardnetDayLocation = time.FixedZone("UTC+8", 8*3600)

type CardnetClient struct {
	ID               int64    `json:"id"`
	Name             string   `json:"name"`
	KeyPrefix        string   `json:"key_prefix"`
	Status           string   `json:"status"`
	AllowedPlans     []string `json:"allowed_plans"`
	AllowedCountries []string `json:"allowed_countries"`
	DailyCodeLimit   int      `json:"daily_code_limit"`
	IPAllowlist      []string `json:"ip_allowlist"`
	LastUsedAt       string   `json:"last_used_at,omitempty"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
}

// AllowsPlan 套餐须显式授权；空清单表示什么都不能发。
func (c CardnetClient) AllowsPlan(plan string) bool {
	return containsFold(c.AllowedPlans, plan)
}

// CardnetDefaultCountry 卡台发码不指定地区时的默认付款地区。
const CardnetDefaultCountry = "PH"

// EffectiveCountries 授权地区；未配置时只有卡台默认地区。
func (c CardnetClient) EffectiveCountries() []string {
	if len(c.AllowedCountries) == 0 {
		return []string{CardnetDefaultCountry}
	}
	return c.AllowedCountries
}

// AllowsCountry 空地区按卡台默认地区计：卡网通常会显式带上 PH，两种写法必须同一结论。
func (c CardnetClient) AllowsCountry(country string) bool {
	country = strings.ToUpper(strings.TrimSpace(country))
	if country == "" {
		country = CardnetDefaultCountry
	}
	return containsFold(c.EffectiveCountries(), country)
}

// AllowsIP 未配置白名单时不限制来源。
func (c CardnetClient) AllowsIP(ip string) bool {
	if len(c.IPAllowlist) == 0 {
		return true
	}
	return containsFold(c.IPAllowlist, ip)
}

type CardnetClientInput struct {
	Name             string
	AllowedPlans     []string
	AllowedCountries []string
	DailyCodeLimit   int
	IPAllowlist      []string
}

type CardnetIssueRequest struct {
	ID              int64  `json:"id"`
	RequestID       string `json:"request_id"`
	ClientID        int64  `json:"client_id"`
	ClientName      string `json:"client_name,omitempty"`
	IdempotencyKey  string `json:"idempotency_key"`
	ParamsHash      string `json:"-"`
	Plan            string `json:"plan"`
	PaymentCountry  string `json:"payment_country"`
	Count           int    `json:"count"`
	AccountID       int64  `json:"account_id"`
	UpstreamIdemKey string `json:"-"`
	ExternalOrderNo string `json:"external_order_no"`
	ExternalRef     string `json:"external_ref"`
	Status          string `json:"status"`
	FeeTotalMinor   int64  `json:"fee_total_minor"`
	ErrorCode       string `json:"error_code,omitempty"`
	ErrorMessage    string `json:"error_message,omitempty"`
	Attempts        int    `json:"attempts"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	CompletedAt     string `json:"completed_at,omitempty"`
}

// CardnetIssueParams 建单参数。ParamsHash 由 plan/count/payment_country 决定。
type CardnetIssueParams struct {
	ClientID        int64
	IdempotencyKey  string
	Plan            string
	PaymentCountry  string
	Count           int
	AccountID       int64
	ExternalOrderNo string
	ExternalRef     string
	DailyCodeLimit  int
}

// CardnetIssuedCode 一张已落库的卡网码。
type CardnetIssuedCode struct {
	ID             int64  `json:"id"`
	UpstreamID     int64  `json:"-"`
	Code           string `json:"code"`
	Plan           string `json:"plan"`
	PaymentCountry string `json:"payment_country"`
	FeeAmountMinor int64  `json:"fee_amount_minor"`
	Status         string `json:"status"`
	AccountID      int64  `json:"-"`
	PromoOnly      bool   `json:"promo_only"`
}

// CardnetCodeToSave 卡台返回、待随请求一起提交的码。
type CardnetCodeToSave struct {
	UpstreamID     int64
	Code           string
	Prefix         string
	Plan           string
	FeeAmountMinor int64
	// PromoOnly 卡网 Grok 首月半价码（请求档位带 _promo），落库为 promo_only=1。
	PromoOnly bool
}

func migrateCardnet() error {
	if DB == nil {
		return nil
	}
	queries := []string{
		// 本站预览拿到的 redemption_token → 码，只增不改（见 RecordPreviewToken）。
		`CREATE TABLE IF NOT EXISTS cdk_preview_tokens (
			redemption_token TEXT PRIMARY KEY,
			cdk_code TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS cardnet_clients (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			key_prefix TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL DEFAULT 'active',
			allowed_plans TEXT NOT NULL DEFAULT '',
			allowed_countries TEXT NOT NULL DEFAULT '',
			daily_code_limit INTEGER NOT NULL DEFAULT 200,
			ip_allowlist TEXT NOT NULL DEFAULT '',
			last_used_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS cardnet_issue_requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id TEXT NOT NULL UNIQUE,
			client_id INTEGER NOT NULL,
			idempotency_key TEXT NOT NULL,
			params_hash TEXT NOT NULL,
			plan TEXT NOT NULL,
			payment_country TEXT NOT NULL DEFAULT '',
			count INTEGER NOT NULL,
			account_id INTEGER NOT NULL,
			upstream_idem_key TEXT NOT NULL,
			external_order_no TEXT NOT NULL DEFAULT '',
			external_ref TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			fee_total_minor INTEGER NOT NULL DEFAULT 0,
			error_code TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			attempts INTEGER NOT NULL DEFAULT 0,
			lease_until DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME,
			UNIQUE(client_id, idempotency_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cnr_client_created ON cardnet_issue_requests(client_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_cnr_status ON cardnet_issue_requests(status)`,
		`CREATE INDEX IF NOT EXISTS idx_cnr_external_order ON cardnet_issue_requests(external_order_no)`,
	}
	var previewTokensExisted int
	_ = DB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='cdk_preview_tokens'`).Scan(&previewTokensExisted)
	for _, q := range queries {
		if _, err := DB.Exec(q); err != nil {
			return err
		}
	}
	if previewTokensExisted == 0 {
		// 首次建表时从现有绑定回填一次：上线前预览过、还没兑换的 Grok 用户不用重新预览。
		// 只能回填这一次——那时还没有半价码，绑定被改挂也无所谓；之后的改挂不能再导进来。
		var bindings int
		_ = DB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='cdk_session_bindings'`).Scan(&bindings)
		if bindings > 0 {
			if _, err := DB.Exec(`INSERT OR IGNORE INTO cdk_preview_tokens (redemption_token, cdk_code)
				SELECT TRIM(redemption_token), cdk_code FROM cdk_session_bindings
				WHERE TRIM(COALESCE(redemption_token,'')) != '' AND cdk_code NOT LIKE 'RT:%'`); err != nil {
				return err
			}
		}
	}
	cols := []struct{ name, ddl string }{
		{"issue_source", `ALTER TABLE cardplatform_cdk_codes ADD COLUMN issue_source TEXT NOT NULL DEFAULT ''`},
		{"cardnet_client_id", `ALTER TABLE cardplatform_cdk_codes ADD COLUMN cardnet_client_id INTEGER NOT NULL DEFAULT 0`},
		{"cardnet_request_id", `ALTER TABLE cardplatform_cdk_codes ADD COLUMN cardnet_request_id INTEGER NOT NULL DEFAULT 0`},
		{"external_order_no", `ALTER TABLE cardplatform_cdk_codes ADD COLUMN external_order_no TEXT NOT NULL DEFAULT ''`},
		// 卡网 Grok 首月半价码：码上存正价档位，半价记在这里（兑换拦截、后台展示都读它）。
		{"promo_only", `ALTER TABLE cardplatform_cdk_codes ADD COLUMN promo_only INTEGER NOT NULL DEFAULT 0`},
	}
	for _, col := range cols {
		var n int
		if err := DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('cardplatform_cdk_codes') WHERE name = ?`, col.name).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := DB.Exec(col.ddl); err != nil {
				return err
			}
		}
	}
	// 加列之前发出的半价码：按发码请求的 _promo 档位补标记。可重复执行。
	if _, err := DB.Exec(`UPDATE cardplatform_cdk_codes SET promo_only = 1
		WHERE promo_only = 0 AND cardnet_request_id IN (
			SELECT id FROM cardnet_issue_requests WHERE plan LIKE 'grok\_%\_promo' ESCAPE '\')`); err != nil {
		return err
	}
	for _, q := range []string{
		`CREATE INDEX IF NOT EXISTS idx_cp_cdk_cardnet_request ON cardplatform_cdk_codes(cardnet_request_id)`,
		`CREATE INDEX IF NOT EXISTS idx_cp_cdk_issue_source ON cardplatform_cdk_codes(issue_source)`,
	} {
		if _, err := DB.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// ---- 客户端 ----

func GenerateCardnetKeyPlain() string {
	return cardnetKeyPrefix + randomHex(32)
}

func IsCardnetKey(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), cardnetKeyPrefix)
}

func hashCardnetKey(plain string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(plain)))
	return hex.EncodeToString(sum[:])
}

func cardnetKeyDisplayPrefix(plain string) string {
	if len(plain) > 16 {
		return plain[:16]
	}
	return plain
}

const cardnetClientCols = `id, name, key_prefix, status, allowed_plans, allowed_countries, daily_code_limit,
	ip_allowlist, COALESCE(last_used_at,''), COALESCE(created_at,''), COALESCE(updated_at,'')`

func scanCardnetClient(s interface{ Scan(...any) error }) (CardnetClient, error) {
	var c CardnetClient
	var plans, countries, ips string
	err := s.Scan(&c.ID, &c.Name, &c.KeyPrefix, &c.Status, &plans, &countries, &c.DailyCodeLimit,
		&ips, &c.LastUsedAt, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return c, err
	}
	c.AllowedPlans = splitList(plans, true)
	c.AllowedCountries = splitList(countries, false)
	c.IPAllowlist = splitList(ips, false)
	return c, nil
}

// CreateCardnetClient 新建客户端，返回只展示一次的明文 Key。
func CreateCardnetClient(in CardnetClientInput) (CardnetClient, string, error) {
	if DB == nil {
		return CardnetClient{}, "", fmt.Errorf("db not ready")
	}
	in, err := normalizeCardnetClientInput(in)
	if err != nil {
		return CardnetClient{}, "", err
	}
	plain := GenerateCardnetKeyPlain()
	res, err := DB.Exec(`
		INSERT INTO cardnet_clients
			(name, key_prefix, key_hash, status, allowed_plans, allowed_countries, daily_code_limit, ip_allowlist)
		VALUES (?, ?, ?, 'active', ?, ?, ?, ?)
	`, in.Name, cardnetKeyDisplayPrefix(plain), hashCardnetKey(plain),
		joinList(in.AllowedPlans), joinList(in.AllowedCountries), in.DailyCodeLimit, joinList(in.IPAllowlist))
	if err != nil {
		return CardnetClient{}, "", err
	}
	id, _ := res.LastInsertId()
	c, err := GetCardnetClient(id)
	return c, plain, err
}

// UpdateCardnetClient 修改授权范围与额度；不改 Key。
func UpdateCardnetClient(id int64, in CardnetClientInput) (CardnetClient, error) {
	if DB == nil || id <= 0 {
		return CardnetClient{}, fmt.Errorf("invalid client id")
	}
	in, err := normalizeCardnetClientInput(in)
	if err != nil {
		return CardnetClient{}, err
	}
	res, err := DB.Exec(`
		UPDATE cardnet_clients
		SET name = ?, allowed_plans = ?, allowed_countries = ?, daily_code_limit = ?, ip_allowlist = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, in.Name, joinList(in.AllowedPlans), joinList(in.AllowedCountries), in.DailyCodeLimit, joinList(in.IPAllowlist), id)
	if err != nil {
		return CardnetClient{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return CardnetClient{}, ErrCardnetNotFound
	}
	return GetCardnetClient(id)
}

func SetCardnetClientStatus(id int64, status string) error {
	if DB == nil || id <= 0 {
		return fmt.Errorf("invalid client id")
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status != CardnetClientActive && status != CardnetClientDisabled {
		return fmt.Errorf("status must be active or disabled")
	}
	res, err := DB.Exec(`UPDATE cardnet_clients SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, status, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrCardnetNotFound
	}
	return nil
}

// RotateCardnetClientKey 换新 Key，旧 Key 立即失效。
func RotateCardnetClientKey(id int64) (string, error) {
	if DB == nil || id <= 0 {
		return "", fmt.Errorf("invalid client id")
	}
	plain := GenerateCardnetKeyPlain()
	res, err := DB.Exec(`
		UPDATE cardnet_clients SET key_prefix = ?, key_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, cardnetKeyDisplayPrefix(plain), hashCardnetKey(plain), id)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", ErrCardnetNotFound
	}
	return plain, nil
}

func GetCardnetClient(id int64) (CardnetClient, error) {
	if DB == nil {
		return CardnetClient{}, fmt.Errorf("db not ready")
	}
	c, err := scanCardnetClient(DB.QueryRow(`SELECT `+cardnetClientCols+` FROM cardnet_clients WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrCardnetNotFound
	}
	return c, err
}

func ListCardnetClients() ([]CardnetClient, error) {
	if DB == nil {
		return nil, fmt.Errorf("db not ready")
	}
	rows, err := DB.Query(`SELECT ` + cardnetClientCols + ` FROM cardnet_clients ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CardnetClient{}
	for rows.Next() {
		c, err := scanCardnetClient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// LookupCardnetClientByKey 按明文 Key 找客户端（不论状态，由调用方判断是否停用）。
func LookupCardnetClientByKey(plain string) (CardnetClient, error) {
	if DB == nil || !IsCardnetKey(plain) {
		return CardnetClient{}, ErrCardnetNotFound
	}
	c, err := scanCardnetClient(DB.QueryRow(`SELECT `+cardnetClientCols+` FROM cardnet_clients WHERE key_hash = ?`, hashCardnetKey(plain)))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrCardnetNotFound
	}
	return c, err
}

func TouchCardnetClient(id int64) {
	if DB == nil || id <= 0 {
		return
	}
	_, _ = DB.Exec(`UPDATE cardnet_clients SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
}

func normalizeCardnetClientInput(in CardnetClientInput) (CardnetClientInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return in, fmt.Errorf("名称不能为空")
	}
	if in.DailyCodeLimit < 0 {
		return in, fmt.Errorf("每日额度不能小于 0")
	}
	in.AllowedPlans = normalizeList(in.AllowedPlans, true)
	in.AllowedCountries = normalizeList(in.AllowedCountries, false)
	for _, c := range in.AllowedCountries {
		if len(c) != 2 {
			return in, fmt.Errorf("地区应为两位国家代码：%s", c)
		}
	}
	in.IPAllowlist = normalizeList(in.IPAllowlist, false)
	for i, ip := range in.IPAllowlist {
		in.IPAllowlist[i] = strings.ToLower(ip)
	}
	return in, nil
}

// ---- 发码请求 ----

func CardnetParamsHash(plan, country string, count int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s", strings.ToLower(strings.TrimSpace(plan)), count,
		strings.ToUpper(strings.TrimSpace(country)))))
	return hex.EncodeToString(sum[:])
}

func newCardnetRequestID() string {
	return "CNR" + time.Now().In(cardnetDayLocation).Format("20060102") + randomHex(6)
}

// CardnetDayStartUTC 当前北京时间自然日的起点（UTC，与 CURRENT_TIMESTAMP 同格式）。
func CardnetDayStartUTC(now time.Time) string {
	local := now.In(cardnetDayLocation)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, cardnetDayLocation)
	return start.UTC().Format("2006-01-02 15:04:05")
}

const cardnetRequestCols = `r.id, r.request_id, r.client_id, COALESCE(c.name,''), r.idempotency_key, r.params_hash,
	r.plan, r.payment_country, r.count, r.account_id, r.upstream_idem_key, r.external_order_no, r.external_ref,
	r.status, r.fee_total_minor, r.error_code, r.error_message, r.attempts,
	COALESCE(r.created_at,''), COALESCE(r.updated_at,''), COALESCE(r.completed_at,'')`

const cardnetRequestFrom = ` FROM cardnet_issue_requests r LEFT JOIN cardnet_clients c ON c.id = r.client_id`

func scanCardnetRequest(s interface{ Scan(...any) error }) (CardnetIssueRequest, error) {
	var r CardnetIssueRequest
	err := s.Scan(&r.ID, &r.RequestID, &r.ClientID, &r.ClientName, &r.IdempotencyKey, &r.ParamsHash,
		&r.Plan, &r.PaymentCountry, &r.Count, &r.AccountID, &r.UpstreamIdemKey, &r.ExternalOrderNo, &r.ExternalRef,
		&r.Status, &r.FeeTotalMinor, &r.ErrorCode, &r.ErrorMessage, &r.Attempts,
		&r.CreatedAt, &r.UpdatedAt, &r.CompletedAt)
	return r, err
}

func getCardnetRequest(where string, args ...any) (CardnetIssueRequest, error) {
	if DB == nil {
		return CardnetIssueRequest{}, fmt.Errorf("db not ready")
	}
	r, err := scanCardnetRequest(DB.QueryRow(`SELECT `+cardnetRequestCols+cardnetRequestFrom+` WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrCardnetNotFound
	}
	return r, err
}

func GetCardnetRequest(id int64) (CardnetIssueRequest, error) {
	return getCardnetRequest(`r.id = ?`, id)
}

func GetCardnetRequestByKey(clientID int64, key string) (CardnetIssueRequest, error) {
	return getCardnetRequest(`r.client_id = ? AND r.idempotency_key = ?`, clientID, key)
}

// CreateOrGetCardnetRequest 幂等建单：同客户端同键已存在时返回已有行（created=false）。
//
// 额度检查与插入是同一条 INSERT ... SELECT：SQLite 单条语句原子执行，并发请求不会一起越过额度。
// 已失败（未发码）的请求不占额度。
func CreateOrGetCardnetRequest(p CardnetIssueParams) (CardnetIssueRequest, bool, error) {
	if DB == nil {
		return CardnetIssueRequest{}, false, fmt.Errorf("db not ready")
	}
	if existing, err := GetCardnetRequestByKey(p.ClientID, p.IdempotencyKey); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, ErrCardnetNotFound) {
		return CardnetIssueRequest{}, false, err
	}
	requestID := newCardnetRequestID()
	upstreamKey := fmt.Sprintf("cn-%d-%s", p.ClientID, requestID)
	res, err := DB.Exec(`
		INSERT OR IGNORE INTO cardnet_issue_requests
			(request_id, client_id, idempotency_key, params_hash, plan, payment_country, count, account_id,
			 upstream_idem_key, external_order_no, external_ref, status)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		WHERE (
			SELECT COALESCE(SUM(count), 0) FROM cardnet_issue_requests
			WHERE client_id = ? AND created_at >= ? AND status != ?
		) + ? <= ?
	`, requestID, p.ClientID, p.IdempotencyKey, CardnetParamsHash(p.Plan, p.PaymentCountry, p.Count),
		strings.ToLower(strings.TrimSpace(p.Plan)), strings.ToUpper(strings.TrimSpace(p.PaymentCountry)), p.Count, p.AccountID,
		upstreamKey, strings.TrimSpace(p.ExternalOrderNo), strings.TrimSpace(p.ExternalRef), CardnetRequestPending,
		p.ClientID, CardnetDayStartUTC(time.Now()), CardnetRequestFailed, p.Count, p.DailyCodeLimit)
	if err != nil {
		return CardnetIssueRequest{}, false, err
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		r, err := GetCardnetRequestByKey(p.ClientID, p.IdempotencyKey)
		return r, true, err
	}
	// 没插进去：要么同键被并发请求抢先插入，要么超额度。
	if existing, err := GetCardnetRequestByKey(p.ClientID, p.IdempotencyKey); err == nil {
		return existing, false, nil
	}
	return CardnetIssueRequest{}, false, ErrCardnetQuotaExceeded
}

// CardnetIssuedToday 当日已占用的张数（不含失败请求）。
func CardnetIssuedToday(clientID int64) (int, error) {
	if DB == nil {
		return 0, fmt.Errorf("db not ready")
	}
	var n int
	err := DB.QueryRow(`
		SELECT COALESCE(SUM(count), 0) FROM cardnet_issue_requests
		WHERE client_id = ? AND created_at >= ? AND status != ?
	`, clientID, CardnetDayStartUTC(time.Now()), CardnetRequestFailed).Scan(&n)
	return n, err
}

// ClaimCardnetRequest 占用一个 pending 请求去调卡台；同一时刻只允许一个调用。
func ClaimCardnetRequest(id int64, lease time.Duration) (bool, error) {
	if DB == nil || id <= 0 {
		return false, fmt.Errorf("invalid request id")
	}
	res, err := DB.Exec(`
		UPDATE cardnet_issue_requests
		SET lease_until = datetime('now', ?), attempts = attempts + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status IN (?, ?) AND (lease_until IS NULL OR lease_until <= datetime('now'))
	`, fmt.Sprintf("+%d seconds", int(lease.Seconds())), id, CardnetRequestPending, CardnetRequestReview)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ReleaseCardnetRequest 结果待定：释放占用、记录原因，保持原状态等待同键重试。
func ReleaseCardnetRequest(id int64, code, message string) error {
	if DB == nil {
		return nil
	}
	_, err := DB.Exec(`
		UPDATE cardnet_issue_requests
		SET lease_until = NULL, error_code = ?, error_message = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, code, truncateText(message, 500), id)
	return err
}

// FailCardnetRequest 明确失败（未发码）。只从 pending/review 转出，不会覆盖成功结果。
func FailCardnetRequest(id int64, code, message string) (bool, error) {
	return finishCardnetRequest(id, CardnetRequestFailed, code, message)
}

// MarkCardnetRequestReview 异常结果待人工核对。
func MarkCardnetRequestReview(id int64, code, message string) (bool, error) {
	return finishCardnetRequest(id, CardnetRequestReview, code, message)
}

func finishCardnetRequest(id int64, status, code, message string) (bool, error) {
	if DB == nil {
		return false, fmt.Errorf("db not ready")
	}
	completed := any(nil)
	if status == CardnetRequestFailed {
		completed = time.Now().UTC().Format("2006-01-02 15:04:05")
	}
	res, err := DB.Exec(`
		UPDATE cardnet_issue_requests
		SET status = ?, error_code = ?, error_message = ?, lease_until = NULL,
		    completed_at = COALESCE(?, completed_at), updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status IN (?, ?)
	`, status, code, truncateText(message, 500), completed, id, CardnetRequestPending, CardnetRequestReview)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// CompleteCardnetRequest 码与请求状态在同一事务提交：要么本站有码且请求成功，要么都没有，
// 不会出现「码落库了请求还是 pending」或反过来的中间态。
func CompleteCardnetRequest(req CardnetIssueRequest, codes []CardnetCodeToSave) error {
	if DB == nil {
		return fmt.Errorf("db not ready")
	}
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var fee int64
	for _, it := range codes {
		plan := strings.TrimSpace(it.Plan)
		if plan == "" {
			plan = req.Plan
		}
		if err := saveCardplatformCDKCode(tx, it.UpstreamID, it.Code, it.Prefix, plan, it.FeeAmountMinor, "unused",
			req.AccountID, req.PaymentCountry); err != nil {
			return err
		}
		res, err := tx.Exec(`
			UPDATE cardplatform_cdk_codes
			SET issue_source = ?, cardnet_client_id = ?, cardnet_request_id = ?, external_order_no = ?, promo_only = ?
			WHERE code = ? AND COALESCE(cardnet_request_id, 0) IN (0, ?)
		`, CardnetIssueSource, req.ClientID, req.ID, req.ExternalOrderNo, boolToInt(it.PromoOnly), strings.TrimSpace(it.Code), req.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			// 同一张上游码已属于另一个卡网请求：绝不能交给第二个订单。
			return ErrCardnetCodeConflict
		}
		fee += it.FeeAmountMinor
	}
	res, err := tx.Exec(`
		UPDATE cardnet_issue_requests
		SET status = ?, fee_total_minor = ?, error_code = '', error_message = '', lease_until = NULL,
		    completed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status IN (?, ?)
	`, CardnetRequestSucceeded, fee, req.ID, CardnetRequestPending, CardnetRequestReview)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrCardnetNotPending
	}
	return tx.Commit()
}

const cardnetCodeCols = `id, COALESCE(upstream_id,0), code, COALESCE(plan,''), COALESCE(payment_country,''),
	COALESCE(fee_amount_minor,0), COALESCE(status,''), COALESCE(fulfilled_account_id,0), COALESCE(promo_only,0) = 1`

func scanCardnetCode(s interface{ Scan(...any) error }) (CardnetIssuedCode, error) {
	var c CardnetIssuedCode
	err := s.Scan(&c.ID, &c.UpstreamID, &c.Code, &c.Plan, &c.PaymentCountry, &c.FeeAmountMinor, &c.Status, &c.AccountID, &c.PromoOnly)
	return c, err
}

// ListCardnetRequestCodes 某请求发出的码（按落库顺序）。
func ListCardnetRequestCodes(requestRowID int64) ([]CardnetIssuedCode, error) {
	if DB == nil {
		return nil, fmt.Errorf("db not ready")
	}
	rows, err := DB.Query(`SELECT `+cardnetCodeCols+` FROM cardplatform_cdk_codes
		WHERE cardnet_request_id = ? ORDER BY id`, requestRowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CardnetIssuedCode{}
	for rows.Next() {
		c, err := scanCardnetCode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCardnetCodeForClient 按本站行 id 取码，且必须属于该客户端。
func GetCardnetCodeForClient(id, clientID int64) (CardnetIssuedCode, error) {
	if DB == nil {
		return CardnetIssuedCode{}, fmt.Errorf("db not ready")
	}
	c, err := scanCardnetCode(DB.QueryRow(`SELECT `+cardnetCodeCols+` FROM cardplatform_cdk_codes
		WHERE id = ? AND cardnet_client_id = ? AND issue_source = ?`, id, clientID, CardnetIssueSource))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrCardnetNotFound
	}
	return c, err
}

// PromoCodePlan 卡网 Grok 首月半价码（promo_only=1）返回它在库里的档位（正价键）。
// 不是半价码（含本站没有的码）promo=false、err=nil。用户输入的码大小写不定，原样和大写各查一次（走 code 唯一索引）。
func PromoCodePlan(code string) (plan string, promo bool, err error) {
	if DB == nil {
		return "", false, fmt.Errorf("db not ready")
	}
	raw := strings.TrimSpace(code)
	if raw == "" {
		return "", false, nil
	}
	var flag int
	err = DB.QueryRow(`SELECT COALESCE(plan,''), COALESCE(promo_only,0) FROM cardplatform_cdk_codes
		WHERE code IN (?, ?) LIMIT 1`, raw, normalizeCDKCode(raw)).Scan(&plan, &flag)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return plan, flag == 1, nil
}

// RecordPreviewToken 本站调卡台预览成功后记下 token 属于哪张码。
// cdk_session_bindings 里的 token 会被预检、换号重新预览改写，不能用来判断「这个 token 兑的是哪张码」；
// 这张表只在本站亲自拿到 token 时写入，已有的行不覆盖，客户端没法把一个 token 改挂到别的码上。
func RecordPreviewToken(code, redemptionToken string) error {
	if DB == nil {
		return fmt.Errorf("db not ready")
	}
	c, tok := normalizeCDKCode(code), strings.TrimSpace(redemptionToken)
	if c == "" || tok == "" {
		return nil
	}
	_, err := DB.Exec(`INSERT OR IGNORE INTO cdk_preview_tokens (redemption_token, cdk_code) VALUES (?, ?)`, tok, c)
	return err
}

// PreviewTokenCode 本站预览发出的 token 对应的码；不是本站预览拿到的 token 返回 ErrCardnetNotFound。
func PreviewTokenCode(redemptionToken string) (string, error) {
	if DB == nil {
		return "", fmt.Errorf("db not ready")
	}
	tok := strings.TrimSpace(redemptionToken)
	if tok == "" {
		return "", ErrCardnetNotFound
	}
	var code string
	err := DB.QueryRow(`SELECT cdk_code FROM cdk_preview_tokens WHERE redemption_token = ?`, tok).Scan(&code)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrCardnetNotFound
	}
	return code, err
}

type CardnetRequestQuery struct {
	ClientID int64
	Status   string
	Query    string // request_id / 卡网订单号 / 幂等键
	Product  string // gpt / grok：库里 Grok 档位一律带 grok_ 前缀
	Page     int
	PageSize int
}

func ListCardnetRequests(q CardnetRequestQuery) ([]CardnetIssueRequest, int, error) {
	if DB == nil {
		return nil, 0, fmt.Errorf("db not ready")
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 100 {
		q.PageSize = 20
	}
	where := []string{"1=1"}
	args := []any{}
	if q.ClientID > 0 {
		where = append(where, "r.client_id = ?")
		args = append(args, q.ClientID)
	}
	if s := strings.TrimSpace(q.Status); s != "" {
		where = append(where, "r.status = ?")
		args = append(args, s)
	}
	if s := strings.TrimSpace(q.Query); s != "" {
		where = append(where, "(r.request_id = ? OR r.external_order_no = ? OR r.idempotency_key = ?)")
		args = append(args, s, s, s)
	}
	switch strings.ToLower(strings.TrimSpace(q.Product)) {
	case "grok":
		where = append(where, `r.plan LIKE 'grok\_%' ESCAPE '\'`)
	case "gpt":
		where = append(where, `r.plan NOT LIKE 'grok\_%' ESCAPE '\'`)
	}
	cond := strings.Join(where, " AND ")
	var total int
	if err := DB.QueryRow(`SELECT COUNT(*)`+cardnetRequestFrom+` WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := DB.Query(`SELECT `+cardnetRequestCols+cardnetRequestFrom+` WHERE `+cond+
		` ORDER BY r.id DESC LIMIT ? OFFSET ?`, append(args, q.PageSize, (q.Page-1)*q.PageSize)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []CardnetIssueRequest{}
	for rows.Next() {
		r, err := scanCardnetRequest(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// CardnetStatRow 按北京时间日期 + 套餐汇总的卡网发码记账。
type CardnetStatRow struct {
	Date     string `json:"date"`
	ClientID int64  `json:"client_id"`
	Plan     string `json:"plan"`
	Codes    int    `json:"codes"`
	FeeMinor int64  `json:"fee_minor"`
	Consumed int    `json:"consumed"`
	Disabled int    `json:"disabled"`
}

// CardnetStats 汇总卡网发出的码与上游服务费。from/to 为北京时间日期（含），空表示不限。
func CardnetStats(clientID int64, from, to string) ([]CardnetStatRow, error) {
	if DB == nil {
		return nil, fmt.Errorf("db not ready")
	}
	where := []string{"issue_source = ?"}
	args := []any{CardnetIssueSource}
	if clientID > 0 {
		where = append(where, "cardnet_client_id = ?")
		args = append(args, clientID)
	}
	day := `date(created_at, '+8 hours')`
	// 半价码单独成行：档位显示成卡网的 _promo 键，与请求列表一致。
	statPlan := `CASE WHEN COALESCE(promo_only,0) = 1 THEN COALESCE(plan,'') || '_promo' ELSE COALESCE(plan,'') END`
	if s := strings.TrimSpace(from); s != "" {
		where = append(where, day+" >= ?")
		args = append(args, s)
	}
	if s := strings.TrimSpace(to); s != "" {
		where = append(where, day+" <= ?")
		args = append(args, s)
	}
	rows, err := DB.Query(`
		SELECT `+day+` AS d, cardnet_client_id, `+statPlan+` AS sp, COUNT(*), COALESCE(SUM(fee_amount_minor),0),
		       SUM(CASE WHEN lower(COALESCE(status,'')) IN ('consumed','used','redeemed') THEN 1 ELSE 0 END),
		       SUM(CASE WHEN lower(COALESCE(status,'')) = 'disabled' THEN 1 ELSE 0 END)
		FROM cardplatform_cdk_codes
		WHERE `+strings.Join(where, " AND ")+`
		GROUP BY d, cardnet_client_id, sp
		ORDER BY d DESC, cardnet_client_id, sp
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CardnetStatRow{}
	for rows.Next() {
		var r CardnetStatRow
		if err := rows.Scan(&r.Date, &r.ClientID, &r.Plan, &r.Codes, &r.FeeMinor, &r.Consumed, &r.Disabled); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- 小工具 ----

func splitList(raw string, lower bool) []string {
	out := []string{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if lower {
			item = strings.ToLower(item)
		} else {
			item = strings.ToUpper(item)
		}
		out = append(out, item)
	}
	return out
}

func normalizeList(items []string, lower bool) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if lower {
			item = strings.ToLower(item)
		} else {
			item = strings.ToUpper(item)
		}
		if seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func joinList(items []string) string {
	return strings.Join(items, ",")
}

func containsFold(items []string, v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	for _, item := range items {
		if strings.EqualFold(item, v) {
			return true
		}
	}
	return false
}

func truncateText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
