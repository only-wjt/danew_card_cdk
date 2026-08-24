package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"strings"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
	"github.com/danew/cdk-recharge-system/internal/db"
)

// issueCDKsForAgentOrder 调卡台发码并落库，返回完整卡密列表（与管理员发码同路径）。
func issueCDKsForAgentOrder(ctx context.Context, plan string, count int, idem string) ([]string, error) {
	plan = strings.TrimSpace(plan)
	if plan == "" {
		return nil, fmt.Errorf("plan required")
	}
	if count < 1 {
		return nil, fmt.Errorf("count must be >= 1")
	}
	cli := cardplatform.NewFromSettings()
	if cli == nil || cardplatform.LoadConfig().APIKey == "" {
		return nil, fmt.Errorf("卡台未配置")
	}
	if idem == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		idem = "agent-order-" + hex.EncodeToString(b)
	}

	var issuePrefs []cardplatform.IssueCardPref
	policy := loadSiteRedeemPolicy()
	if issuer, segType, segKey := resolveIssueCardPref(policy); segKey != "" || issuer != "" {
		issuePrefs = append(issuePrefs, cardplatform.IssueCardPref{
			Issuer: issuer, SegmentType: segType, SegmentKey: segKey,
		})
	} else if rules, err := db.GetCardSelectionRules(); err == nil {
		for _, r := range rules {
			if !r.Enabled || strings.TrimSpace(r.PlanKey) == "" {
				continue
			}
			iss := strings.ToLower(strings.TrimSpace(r.Channel))
			if iss == "" {
				iss = "one"
			}
			issuePrefs = append(issuePrefs, cardplatform.IssueCardPref{
				Issuer: iss, SegmentType: "product", SegmentKey: strings.TrimSpace(r.PlanKey),
			})
			break
		}
	}

	var res *cardplatform.IssueCDKResult
	var err error
	if len(issuePrefs) > 0 {
		res, err = cli.IssueCDKs(ctx, plan, count, idem, issuePrefs[0])
	} else {
		res, err = cli.IssueCDKs(ctx, plan, count, idem)
	}
	if err != nil {
		return nil, err
	}

	codes := make([]string, 0, len(res.Issued))
	for _, it := range res.Issued {
		code := strings.TrimSpace(it.Code)
		if code == "" {
			log.Printf("[agent-order] upstream incomplete code id=%d prefix=%s", it.ID, it.CodePrefix)
			continue
		}
		prefix := strings.TrimSpace(it.CodePrefix)
		if prefix == "" && len(code) >= 14 {
			prefix = code[:14]
		}
		if err := db.SaveCardplatformCDKCode(it.ID, code, prefix, it.Plan, it.FeeAmountMinor); err != nil {
			log.Printf("[agent-order] save code failed id=%d: %v", it.ID, err)
			return codes, fmt.Errorf("落库失败: %w", err)
		}
		codes = append(codes, code)
	}
	if len(codes) == 0 {
		return nil, fmt.Errorf("卡台未返回完整卡密")
	}
	return codes, nil
}

// fulfillAgentOrder 发码并划给下单代理。
func fulfillAgentOrder(ctx context.Context, order db.AgentOrder) (db.AgentOrder, error) {
	updated, err := db.UpdateAgentOrderIssuing(order.OrderNo)
	if err != nil {
		return order, err
	}
	if !updated {
		fresh, _ := db.GetAgentOrderByNo(order.OrderNo)
		if fresh.Status == db.AgentOrderStatusDelivered {
			return fresh, nil
		}
		if fresh.Status == db.AgentOrderStatusIssuing {
			return fresh, fmt.Errorf("订单正在发货中，请稍后刷新")
		}
		return fresh, fmt.Errorf("订单状态不可发货")
	}
	need := order.Count - order.IssuedCount
	if need <= 0 {
		need = order.Count
	}
	existing := append([]string{}, order.IssuedCodes...)
	idem := "fulfill-" + order.OrderNo
	if order.IssuedCount > 0 {
		idem = idem + "-retry"
	}

	newCodes, err := issueCDKsForAgentOrder(ctx, order.Plan, need, idem)
	if err != nil {
		_ = db.FailAgentOrderDelivery(order.OrderNo, len(existing), existing, err.Error())
		order.FailReason = err.Error()
		order.Status = db.AgentOrderStatusPaidUndelivered
		order.IssuedCodes = existing
		order.IssuedCount = len(existing)
		return order, err
	}
	allCodes := append(existing, newCodes...)
	assigned, skipped, assignErr := db.AssignCDKsToAgent(order.AgentUserID, allCodes)
	if assignErr != nil {
		_ = db.FailAgentOrderDelivery(order.OrderNo, len(allCodes), allCodes, assignErr.Error())
		order.FailReason = assignErr.Error()
		order.Status = db.AgentOrderStatusPaidUndelivered
		order.IssuedCodes = allCodes
		order.IssuedCount = len(allCodes)
		return order, assignErr
	}
	if assigned < len(allCodes) && len(skipped) > 0 {
		msg := strings.Join(skipped, "; ")
		if assigned == 0 {
			_ = db.FailAgentOrderDelivery(order.OrderNo, len(allCodes), allCodes, msg)
			order.FailReason = msg
			order.Status = db.AgentOrderStatusPaidUndelivered
			order.IssuedCodes = allCodes
			order.IssuedCount = len(allCodes)
			return order, fmt.Errorf("%s", msg)
		}
		log.Printf("[agent-order] partial assign order=%s assigned=%d skipped=%v", order.OrderNo, assigned, skipped)
	}

	if len(allCodes) < order.Count {
		reason := fmt.Sprintf("仅发出 %d/%d 张", len(allCodes), order.Count)
		_ = db.FailAgentOrderDelivery(order.OrderNo, len(allCodes), allCodes, reason)
		order.FailReason = reason
		order.Status = db.AgentOrderStatusPaidUndelivered
		order.IssuedCodes = allCodes
		order.IssuedCount = len(allCodes)
		return order, fmt.Errorf("%s", reason)
	}

	if err := db.CompleteAgentOrderDelivery(order.OrderNo, allCodes); err != nil {
		_ = db.FailAgentOrderDelivery(order.OrderNo, len(allCodes), allCodes, err.Error())
		order.FailReason = err.Error()
		order.Status = db.AgentOrderStatusPaidUndelivered
		order.IssuedCodes = allCodes
		order.IssuedCount = len(allCodes)
		return order, err
	}
	order.Status = db.AgentOrderStatusDelivered
	order.IssuedCodes = allCodes
	order.IssuedCount = len(allCodes)
	order.FailReason = ""
	return order, nil
}
