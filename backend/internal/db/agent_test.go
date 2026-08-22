package db

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/danew/cdk-recharge-system/internal/config"
)

func openTestDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("INSTALL_MODE", "wizard")
	if err := Init(&config.DatabaseConfig{}); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() {
		if DB != nil {
			_ = DB.Close()
			DB = nil
		}
	})
}

// 站长创建代理必须能落库并可按用户名取回。
func TestCreateAgentUser(t *testing.T) {
	openTestDB(t)

	agent, err := CreateAgentUser("partner-a", "Str0ngPassw0rd2026", "甲代理", []string{"plus"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if agent.ID == 0 || agent.Username != "partner-a" {
		t.Fatalf("unexpected agent: %+v", agent)
	}
	if agent.Status != "active" {
		t.Fatalf("status = %q, want active", agent.Status)
	}
	if len(agent.AllowedPlans) != 1 || agent.AllowedPlans[0] != "plus" {
		t.Fatalf("allowed plans = %v", agent.AllowedPlans)
	}

	got, hash, err := GetAgentUserByUsername("partner-a")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.ID != agent.ID {
		t.Fatalf("id mismatch: %d vs %d", got.ID, agent.ID)
	}
	if ok, _ := VerifyAdminPassword(hash, "Str0ngPassw0rd2026"); !ok {
		t.Fatal("password verify failed")
	}
}

// 用户名重复时必须报错，而不是静默建出第二个账号。
func TestCreateAgentUserDuplicate(t *testing.T) {
	openTestDB(t)

	if _, err := CreateAgentUser("dup", "Str0ngPassw0rd2026", "", nil); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := CreateAgentUser("dup", "Str0ngPassw0rd2026", "", nil); err == nil {
		t.Fatal("duplicate username should fail")
	}
}

// RandomPassword 生成的口令会直接进入「至少 12 位 + 含字母和数字」的校验，
// 字母表随机取样不保证一定含数字，这里用大样本兜住回归。
func TestRandomPasswordAlwaysPassesStrengthCheck(t *testing.T) {
	for i := 0; i < 2000; i++ {
		p := RandomPassword(16)
		if IsWeakPassword(p) {
			t.Fatalf("weak password generated: %q", p)
		}
		var letter, digit bool
		for _, r := range p {
			if unicode.IsLetter(r) {
				letter = true
			}
			if unicode.IsDigit(r) {
				digit = true
			}
		}
		if !letter || !digit {
			t.Fatalf("generated password missing letter or digit: %q (letter=%v digit=%v)", p, letter, digit)
		}
	}
}

// 代理记录查询必须只返回本代理的数据。
func TestListAgentRechargeRecordsIsolatesAgents(t *testing.T) {
	openTestDB(t)

	mk := func(batchID string, agentID int64, email, sessionHash string) {
		t.Helper()
		batch := AdminRechargeBatch{
			BatchID: batchID, Operator: "agent", AgentUserID: agentID,
			Source: "agent_api", Plan: "plus", Total: 1, Status: "running",
		}
		item := AdminRechargeItem{
			BatchID: batchID, Seq: 1, ClientRequestID: batchID + "-001",
			Plan: "plus", CredMode: "session", AccountEmail: email,
			SessionHash: sessionHash, Status: "success",
		}
		if err := CreateAdminRechargeBatch(batch, batchID, []AdminRechargeItem{item}); err != nil {
			t.Fatalf("create batch %s: %v", batchID, err)
		}
	}
	mk("agt-1", 1, "a@example.com", "hash-a")
	mk("agt-2", 2, "b@example.com", "hash-b")

	list, total, err := ListAgentRechargeRecords(AgentRecordQuery{AgentUserID: 1})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("total=%d len=%d, want 1", total, len(list))
	}
	if list[0].AccountEmail != "a@example.com" {
		t.Fatalf("leaked other agent record: %+v", list[0])
	}

	bySession, total, err := ListAgentRechargeRecords(AgentRecordQuery{AgentUserID: 1, SessionHash: "hash-b"})
	if err != nil {
		t.Fatalf("session search: %v", err)
	}
	if total != 0 || len(bySession) != 0 {
		t.Fatalf("agent 1 must not find agent 2 session: %+v", bySession)
	}
}

// API Key 只存哈希，校验要能反查到所属代理；停用后必须拒绝。
func TestAgentAPIKeyLifecycle(t *testing.T) {
	openTestDB(t)

	agent, err := CreateAgentUser("keyed", "Str0ngPassw0rd2026", "", nil)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	plain := GenerateAgentAPIKeyPlain()
	if !strings.HasPrefix(plain, "ak_live_") {
		t.Fatalf("unexpected key format: %q", plain)
	}
	if _, err := CreateAgentAPIKey(agent.ID, "prod", plain); err != nil {
		t.Fatalf("create key: %v", err)
	}

	id, err := LookupAgentByAPIKey(plain)
	if err != nil || id != agent.ID {
		t.Fatalf("lookup = (%d, %v), want (%d, nil)", id, err, agent.ID)
	}

	if err := UpdateAgentUserStatus(agent.ID, "suspended"); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if _, err := LookupAgentByAPIKey(plain); err == nil {
		t.Fatal("suspended agent key must be rejected")
	}
}
