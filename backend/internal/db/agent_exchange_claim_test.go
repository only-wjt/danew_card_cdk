package db

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestClaimAgentCDKExchangeOnlyOnce(t *testing.T) {
	openTestDB(t)
	const n = 20
	var wins int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ClaimAgentCDKExchange("hash-race", "1.1.1.1"); err == nil {
				atomic.AddInt32(&wins, 1)
			} else if err != ErrExchangeClaimed {
				t.Errorf("unexpected err: %v", err)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("claims won = %d, want 1", wins)
	}
}

func TestReleaseOnlyPendingClaim(t *testing.T) {
	openTestDB(t)
	if err := ClaimAgentCDKExchange("h1", ""); err != nil {
		t.Fatal(err)
	}
	ReleaseAgentCDKExchange("h1")
	if err := ClaimAgentCDKExchange("h1", ""); err != nil {
		t.Fatalf("re-claim after release: %v", err)
	}
	if err := CompleteAgentCDKExchange("h1", 1, 2, "a", "b", "plus", 3, "failed"); err != nil {
		t.Fatal(err)
	}
	ReleaseAgentCDKExchange("h1")
	if !AgentCDKAlreadyExchanged("h1") {
		t.Fatal("completed exchange must not be released")
	}
	if err := ClaimAgentCDKExchange("h1", ""); err != ErrExchangeClaimed {
		t.Fatalf("want ErrExchangeClaimed, got %v", err)
	}
}

func TestMigrateAgentExchangeClaimKeepsDuplicates(t *testing.T) {
	openTestDB(t)
	if _, err := DB.Exec(`DROP INDEX IF EXISTS idx_agent_ex_old_hash_uniq`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := DB.Exec(`INSERT INTO agent_cdk_exchanges (old_code_hash, status) VALUES ('dup', 'done')`); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateAgentExchangeClaim(); err != nil {
		t.Fatal(err)
	}
	var total, exact int
	_ = DB.QueryRow(`SELECT COUNT(*) FROM agent_cdk_exchanges WHERE old_code_hash LIKE 'dup%'`).Scan(&total)
	_ = DB.QueryRow(`SELECT COUNT(*) FROM agent_cdk_exchanges WHERE old_code_hash = 'dup'`).Scan(&exact)
	if total != 3 || exact != 1 {
		t.Fatalf("total=%d exact=%d", total, exact)
	}
}
