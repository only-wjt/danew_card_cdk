package db

import (
	"fmt"
	"strconv"
	"strings"
)

// DeleteCardPlatformAccount 删掉一台卡台账户。
// 只在没有任何在途引用时才允许删；历史记录（已完成的码、回调日志）保留，只清这台的选卡缓存。
func DeleteCardPlatformAccount(id int64) error {
	if DB == nil || id <= 0 {
		return fmt.Errorf("invalid account id")
	}
	acc, err := GetCardPlatformAccount(id)
	if err != nil {
		return err
	}
	if acc.IsPrimaryDefault && acc.Status == "active" && AccountServesOpenAI(acc) {
		return fmt.Errorf("这是 GPT 主台，先到「GPT 发码策略」把别的卡台调到第一位")
	}
	if raw, _ := GetSetting("legacy_card_platform_account_id"); strings.TrimSpace(raw) == strconv.FormatInt(id, 10) {
		return fmt.Errorf("老码还归这台兑换，不能删，只能停用")
	}
	if n, err := CountBindingsByAccount(id); err != nil {
		return err
	} else if n > 0 {
		return fmt.Errorf("还有 %d 张本站码绑在这台上没兑换，先停用，等兑完再删", n)
	}
	if err := migrateXCodes(); err != nil {
		return err
	}
	var pendingX int
	if err := DB.QueryRow(`
		SELECT COUNT(*) FROM x_codes WHERE account_id = ? AND status NOT IN ('completed','disabled')
	`, id).Scan(&pendingX); err != nil {
		return err
	}
	if pendingX > 0 {
		return fmt.Errorf("还有 %d 张 X 码在这台上没兑完，先停用，等兑完或作废后再删", pendingX)
	}
	if err := migrateXMember(); err != nil {
		return err
	}
	var boundOn int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM x_channels WHERE account_id = ? AND enabled = 1`, id).Scan(&boundOn); err != nil {
		return err
	}
	if boundOn > 0 {
		return fmt.Errorf("X 通道还开着并绑在这台上，先到「X 会员 → 供货设置」改绑或关掉")
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE x_channels SET account_id = 0, updated_at = CURRENT_TIMESTAMP WHERE account_id = ?`, id); err != nil {
		return err
	}
	for _, table := range []string{
		"account_card_selection_rules",
		"account_plan_status_cache",
		"account_card_product_cache",
		"account_card_blocklist",
	} {
		// 这几张表按需建，可能还不存在；不存在就跳过。
		var exists int
		_ = tx.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists)
		if exists == 0 {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM `+table+` WHERE account_id = ?`, id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM card_platform_accounts WHERE id = ?`, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return ensurePrimaryAccount()
}
