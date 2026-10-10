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
