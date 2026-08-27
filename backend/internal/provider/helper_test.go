package provider

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/danew/cdk-recharge-system/internal/config"
	"github.com/danew/cdk-recharge-system/internal/db"
)

func withTestDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("INSTALL_MODE", "wizard")
	if err := db.Init(&config.DatabaseConfig{}); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		if db.DB != nil {
			_ = db.DB.Close()
			db.DB = nil
		}
	})
}

// seedAccounts 造 n 个 active 卡台账户，id 从 1 起、priority 递增（1 号是主台）。
func seedAccounts(t *testing.T, n int) {
	t.Helper()
	if _, err := db.DB.Exec(`DELETE FROM card_platform_accounts`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		primary := 0
		if i == 1 {
			primary = 1
		}
		if _, err := db.DB.Exec(`
			INSERT INTO card_platform_accounts
			(id, name, protocol, site_base, cred_secret, status, priority, is_primary_default)
			VALUES (?, ?, ?, ?, ?, 'active', ?, ?)
		`, i, fmt.Sprintf("台%d", i), db.AccountProtocolSpaceXLegacy,
			fmt.Sprintf("https://card%d.example.com", i), fmt.Sprintf("sk_test_%d", i),
			i*10, primary); err != nil {
			t.Fatal(err)
		}
	}
}

// stubBuild 把 adapter 工厂换成假实现，测试不打真网络。
func stubBuild(byAccount map[int64]CardProvider) func() {
	orig := Build
	Build = func(acc db.CardPlatformAccount) (CardProvider, error) {
		if p, ok := byAccount[acc.ID]; ok {
			return p, nil
		}
		return nil, fmt.Errorf("no stub provider for account %d", acc.ID)
	}
	return func() { Build = orig }
}
