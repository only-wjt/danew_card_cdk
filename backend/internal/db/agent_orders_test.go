package db

import (
	"testing"
	"time"
)

func TestAgentOrderExpiry(t *testing.T) {
	openTestDB(t)

	order, err := CreateAgentOrder(AgentOrder{
		OrderNo:          "AGTEST001",
		AgentUserID:      1,
		Plan:             "plus",
		PlanLabel:        "Plus",
		Count:            1,
		UnitPriceCents:   1000,
		TotalAmountCents: 1000,
		PayType:          "alipay",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if order.Status != AgentOrderStatusPendingPay {
		t.Fatalf("status = %s", order.Status)
	}

	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	if _, err := DB.Exec(`UPDATE agent_orders SET expires_at = ? WHERE order_no = ?`, past, order.OrderNo); err != nil {
		t.Fatalf("backdate expires_at: %v", err)
	}

	got, expired, err := ExpireAgentOrderIfNeeded(order.OrderNo)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if !expired || got.Status != AgentOrderStatusExpired {
		t.Fatalf("expected expired, got expired=%v status=%s", expired, got.Status)
	}

	_, marked, err := MarkAgentOrderPaid(order.OrderNo, "T1")
	if err != nil {
		t.Fatalf("mark paid: %v", err)
	}
	if marked {
		t.Fatal("expired order should not mark paid")
	}
}
