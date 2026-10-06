package db

import "strings"

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
