package db

import (
	"database/sql"
	"strings"
)

// 旧行保持 NULL，直到当前页从卡台刷到地区。
// 把 NULL 当成菲律宾会把当时发到别的地区的码标错。
func migrateCardplatformCDKRegionCol() error {
	if DB == nil {
		return nil
	}
	var n int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('cardplatform_cdk_codes') WHERE name='payment_country'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := DB.Exec(`ALTER TABLE cardplatform_cdk_codes ADD COLUMN payment_country TEXT`)
	return err
}

func UpdateCardplatformCDKRegion(upstreamID int64, country string) error {
	return UpdateCardplatformCDKMetadata(upstreamID, "", country)
}

// UpdateCardplatformCDKMetadata 当前页刷新时一次写入状态和发卡地区。
func UpdateCardplatformCDKMetadata(upstreamID int64, status, country string) error {
	if DB == nil || upstreamID <= 0 {
		return nil
	}
	status = strings.ToLower(strings.TrimSpace(status))
	_, err := DB.Exec(`
		UPDATE cardplatform_cdk_codes
		SET payment_country = ?,
		    status = CASE WHEN ? != '' THEN ? ELSE status END
		WHERE upstream_id = ?
	`, strings.ToUpper(strings.TrimSpace(country)), status, status, upstreamID)
	return err
}

// ClaimCDKSuccessNotice 把这张码标成已开通，并且只在第一次成功。
// 回调和轮询都会走到这里，后到的不再发通知。
// country 为 nil 表示地区还没同步；空字符串表示发码时用的默认菲律宾。
func ClaimCDKSuccessNotice(code string) (plan string, country *string, ok bool) {
	if DB == nil {
		return "", nil, false
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return "", nil, false
	}
	var id int64
	var countryNS sql.NullString
	err := DB.QueryRow(`
		SELECT id, COALESCE(plan,''), payment_country
		FROM cardplatform_cdk_codes WHERE code = ? COLLATE NOCASE LIMIT 1
	`, code).Scan(&id, &plan, &countryNS)
	if err != nil || id <= 0 {
		return "", nil, false
	}
	res, err := DB.Exec(`
		UPDATE cardplatform_cdk_codes SET status = 'consumed'
		WHERE id = ? AND lower(COALESCE(status,'')) NOT IN ('consumed','used','disabled')
	`, id)
	if err != nil {
		return "", nil, false
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return "", nil, false
	}
	if countryNS.Valid {
		v := countryNS.String
		country = &v
	}
	return plan, country, true
}

// migrateAdminRechargeBatchRegionCol 批量充值批次记录付款地区（旧批次为空 = 默认菲律宾）。
func migrateAdminRechargeBatchRegionCol() error {
	if DB == nil {
		return nil
	}
	var n int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('admin_recharge_batches') WHERE name='payment_country'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := DB.Exec(`ALTER TABLE admin_recharge_batches ADD COLUMN payment_country TEXT DEFAULT ''`)
	return err
}

// migrateCodeLookupIndexes 给热路径的 upper(trim(code)) 查询建表达式索引。
// 这些查询每次预览/预检/兑换/查结果都会跑 3–5 遍，没索引就是全表扫描（见 C10）。
// 索引表达式必须与查询逐字一致，SQLite 才会用它。
func migrateCodeLookupIndexes() error {
	if DB == nil {
		return nil
	}
	stmts := []string{
		`CREATE INDEX IF NOT EXISTS idx_cpk_code_upper ON cardplatform_cdk_codes(upper(trim(code)))`,
		`CREATE INDEX IF NOT EXISTS idx_ari_cdk_code_upper ON admin_recharge_items(upper(trim(cdk_code)))`,
		`CREATE INDEX IF NOT EXISTS idx_scb_site_code_upper ON site_cdk_bindings(upper(trim(site_code)))`,
		`CREATE INDEX IF NOT EXISTS idx_cdkeys_code_upper ON cd_keys(upper(trim(code)))`,
		`CREATE INDEX IF NOT EXISTS idx_recharge_tasks_cdk_upper ON recharge_tasks(upper(trim(cdk_code)))`,
		// 通知按 code = ? COLLATE NOCASE 读档位/地区（成功与失败通知、结果轮询都会走），
		// BINARY 的 UNIQUE 索引用不上，需要同排序规则的索引。
		`CREATE INDEX IF NOT EXISTS idx_cpk_code_nocase ON cardplatform_cdk_codes(code COLLATE NOCASE)`,
		// 卡台同步曾以裸键落库的 Grok 档位：X 没有这些键，补成 grok_ 前缀（与 NormalizeListedPlan 同一规则）。
		// 裸 premium_* 与 X 同名，判不了，不动。幂等。
		`UPDATE cardplatform_cdk_codes SET plan = 'grok_' || CASE lower(plan)
			WHEN 'lite_monthly' THEN 'plus_monthly' WHEN 'lite_yearly' THEN 'plus_yearly'
			WHEN 'heavy_monthly' THEN 'ultra_monthly' WHEN 'heavy_yearly' THEN 'ultra_yearly'
			WHEN 'monthly' THEN 'premium_monthly' WHEN 'yearly' THEN 'premium_yearly'
			ELSE lower(plan) END
		WHERE lower(plan) IN ('plus_monthly','plus_yearly','super_monthly','super_yearly','ultra_monthly','ultra_yearly',
			'lite_monthly','lite_yearly','heavy_monthly','heavy_yearly','monthly','yearly')`,
	}
	for _, q := range stmts {
		if _, err := DB.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// LookupStoredCDKRegionByCode 按完整码读本站缓存的付款地区，语义同 LookupStoredCDKRegion。
// 多卡台账户时各台的 upstream_id 各自编号、可能重复，已知完整码时必须按码查。
func LookupStoredCDKRegionByCode(code string) (country string, known bool) {
	code = strings.TrimSpace(code)
	if DB == nil || code == "" {
		return "", false
	}
	var v sql.NullString
	if err := DB.QueryRow(`SELECT payment_country FROM cardplatform_cdk_codes WHERE upper(trim(code)) = upper(trim(?)) ORDER BY created_at DESC LIMIT 1`, code).Scan(&v); err != nil || !v.Valid {
		return "", false
	}
	return strings.ToUpper(strings.TrimSpace(v.String)), true
}

// UpdateCardplatformCDKRegionByCode 按完整码回填付款地区（不碰同 upstream_id 的其它账户的码）。
func UpdateCardplatformCDKRegionByCode(code, country string) error {
	code = strings.TrimSpace(code)
	if DB == nil || code == "" {
		return nil
	}
	_, err := DB.Exec(`UPDATE cardplatform_cdk_codes SET payment_country = ? WHERE upper(trim(code)) = upper(trim(?))`,
		strings.ToUpper(strings.TrimSpace(country)), code)
	return err
}

// LookupStoredCDKRegion 读本站缓存的付款地区。known=false 表示老数据没记过地区（NULL），
// 调用方必须去卡台取权威值，不能把未知当成默认菲律宾。空串+known=true 才是明确的默认区。
func LookupStoredCDKRegion(upstreamID int64) (country string, known bool) {
	if DB == nil || upstreamID <= 0 {
		return "", false
	}
	var v sql.NullString
	if err := DB.QueryRow(`SELECT payment_country FROM cardplatform_cdk_codes WHERE upstream_id = ? ORDER BY created_at DESC LIMIT 1`, upstreamID).Scan(&v); err != nil || !v.Valid {
		return "", false
	}
	return strings.ToUpper(strings.TrimSpace(v.String)), true
}
