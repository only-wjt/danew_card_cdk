package db

// AdminUserActive 管理员存在且未停用。
func AdminUserActive(id int64) bool {
	if DB == nil || id <= 0 {
		return false
	}
	var active int
	if err := DB.QueryRow(`SELECT COALESCE(is_active,1) FROM admin_users WHERE id = ?`, id).Scan(&active); err != nil {
		return false
	}
	return active == 1
}
