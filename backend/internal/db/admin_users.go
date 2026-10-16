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

// AgentUserActive 代理存在且状态为 active（JWT 登录态每次请求复查，停用即刻失效）。
func AgentUserActive(id int64) bool {
	if DB == nil || id <= 0 {
		return false
	}
	var status string
	if err := DB.QueryRow(`SELECT COALESCE(status,'') FROM agent_users WHERE id = ?`, id).Scan(&status); err != nil {
		return false
	}
	return status == "active"
}
