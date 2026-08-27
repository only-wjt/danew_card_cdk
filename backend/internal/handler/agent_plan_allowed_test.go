package handler

import (
	"testing"

	"github.com/danew/cdk-recharge-system/internal/db"
)

func TestAgentPlanAllowedWithSellable(t *testing.T) {
	agent := &db.AgentUser{AllowedPlans: []string{"plus", "pro_5x"}}
	sellable := map[string]bool{"plus": true, "pro_5x": true, "pro_20x": true}

	if !agentPlanAllowed(agent, "plus", sellable) {
		t.Fatal("plus in whitelist and sellable")
	}
	if agentPlanAllowed(agent, "pro_20x", sellable) {
		t.Fatal("pro_20x not in agent whitelist")
	}
	if agentPlanAllowed(agent, "ultra", sellable) {
		t.Fatal("ultra not sellable")
	}
	if !agentPlanAllowed(agent, "plus", nil) {
		t.Fatal("nil sellable should not block whitelisted plan")
	}
}

func TestAgentPlanAllowedEmptyWhitelist(t *testing.T) {
	agent := &db.AgentUser{AllowedPlans: nil}
	sellable := map[string]bool{"plus": true}
	if !agentPlanAllowed(agent, "plus", sellable) {
		t.Fatal("empty whitelist follows sellable")
	}
	if agentPlanAllowed(agent, "pro_20x", sellable) {
		t.Fatal("pro_20x not sellable")
	}
}

func TestAgentPlanAllowedLocalStockAlways(t *testing.T) {
	restricted := &db.AgentUser{AllowedPlans: []string{"plus"}}
	sellable := map[string]bool{"plus": true}
	if !agentPlanAllowed(restricted, "gpt_white", sellable) {
		t.Fatal("gpt_white must be allowed even when whitelist set")
	}
	if !agentPlanAllowed(restricted, "gpt_white", nil) {
		t.Fatal("gpt_white must skip card-platform sellable check")
	}
	open := &db.AgentUser{AllowedPlans: nil}
	if !agentPlanAllowed(open, "gpt_white", sellable) {
		t.Fatal("gpt_white must be allowed for all agents")
	}
}
