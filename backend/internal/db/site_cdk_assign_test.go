package db

import "testing"

// 本站 DN- 码的 upstream_id 恒为 0，分配/解绑/占位都必须按主键 id 更新。
// 曾经用 WHERE upstream_id = ? 导致：给代理分配一张 DN- 码会把库里所有 DN- 码一起划走。
func TestAssignSiteCDKDoesNotTouchOtherSiteCodes(t *testing.T) {
	openTestDB(t)
	agent, err := CreateAgentUser("siteagent", "AgentTestPass2026", "Site Agent", nil)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mine, err := CreatePendingSiteCDK("DN-AAAA-AAAA-AAAA", "plus", true, 100)
	if err != nil {
		t.Fatalf("create site cdk 1: %v", err)
	}
	other, err := CreatePendingSiteCDK("DN-BBBB-BBBB-BBBB", "plus", true, 100)
	if err != nil {
		t.Fatalf("create site cdk 2: %v", err)
	}
	// 两张码的 upstream_id 都是 0，正是这个前提让旧实现误伤全部 DN- 码
	var zeroCount int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM cardplatform_cdk_codes WHERE upstream_id = 0`).Scan(&zeroCount); err != nil {
		t.Fatal(err)
	}
	if zeroCount < 2 {
		t.Fatalf("expected >=2 site codes with upstream_id=0, got %d", zeroCount)
	}

	if n, skipped, err := AssignCDKsToAgent(agent.ID, []string{mine.Code}); err != nil || n != 1 {
		t.Fatalf("assign: n=%d skipped=%v err=%v", n, skipped, err)
	}

	if got := assignedAgent(mine.ID); got != agent.ID {
		t.Fatalf("assigned code owner = %d, want %d", got, agent.ID)
	}
	if got := assignedAgent(other.ID); got != 0 {
		t.Fatalf("untouched code was assigned to %d — upstream_id=0 blast radius", got)
	}

	// 解绑同样只能影响这一张
	if _, _, err := UnassignCDKsFromAgent(agent.ID, []string{mine.Code}); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if got := assignedAgent(mine.ID); got != 0 {
		t.Fatalf("owner after unassign = %d", got)
	}
}

// 状态更新不能跨行：Reserve/Release/Consume 都必须只改目标那一张。
func TestSiteCDKStatusUpdatesAreScopedToOneRow(t *testing.T) {
	openTestDB(t)
	target, err := CreatePendingSiteCDK("DN-CCCC-CCCC-CCCC", "plus", true, 100)
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := CreatePendingSiteCDK("DN-DDDD-DDDD-DDDD", "plus", true, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := ActivateSiteCDK(target.ID, 100); err != nil {
		t.Fatal(err)
	}
	if err := ActivateSiteCDK(sibling.ID, 100); err != nil {
		t.Fatal(err)
	}

	// 占位走真实路径；明细直接落终态，这样下面 ReleaseAgentCDK 才不被在途明细挡住
	reserveViaBatch(t, target.Code, "scope-1", "failed")
	if got := cdkStatusByID(target.ID); got != "reserved" {
		t.Fatalf("target status = %q, want reserved", got)
	}
	if got := cdkStatusByID(sibling.ID); got != "unused" {
		t.Fatalf("sibling status = %q, want unused", got)
	}

	if err := ReleaseAgentCDK(target.Code); err != nil {
		t.Fatalf("release: %v", err)
	}
	if got := cdkStatusByID(target.ID); got != "unused" {
		t.Fatalf("after release = %q", got)
	}
	if got := cdkStatusByID(sibling.ID); got != "unused" {
		t.Fatalf("sibling after release = %q", got)
	}
}

func assignedAgent(id int64) int64 {
	var v int64
	_ = DB.QueryRow(`SELECT COALESCE(assigned_agent_user_id,0) FROM cardplatform_cdk_codes WHERE id = ?`, id).Scan(&v)
	return v
}

func cdkStatusByID(id int64) string {
	var v string
	_ = DB.QueryRow(`SELECT COALESCE(status,'') FROM cardplatform_cdk_codes WHERE id = ?`, id).Scan(&v)
	return v
}
