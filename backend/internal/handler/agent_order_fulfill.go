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
	"github.com/danew/cdk-recharge-system/internal/provider"
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
	// 本站统一发码：代理拿到的也是 DN- 码，和普通用户同一套结构，才能一起切台。
	if siteDualBindEnabled() {
		codes, err := provider.DualIssueBatch(ctx, plan, count, allowDegradedSingleBind())
		if err != nil {
			return codes, err
		}
		if len(codes) == 0 {
			return nil, fmt.Errorf("发码失败")
		}
		return codes, nil
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
	issuePlan := resolveCardIssuePlan(plan, agentSellableKeys(ctx))
	if len(issuePrefs) > 0 {
		res, err = cli.IssueCDKs(ctx, issuePlan, count, idem, issuePrefs[0])
	} else {
		res, err = cli.IssueCDKs(ctx, issuePlan, count, idem)
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
		if err := db.SaveCardplatformCDKCodeForAccount(it.ID, code, prefix, it.Plan, it.FeeAmountMinor, "unused", primaryCardAccountID()); err != nil {
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
			enqueueAgentOrderDelivered(fresh)
			return fresh, nil
		}
		if fresh.Status == db.AgentOrderStatusIssuing {
			return fresh, fmt.Errorf("订单正在发货中，请稍后刷新")
		}
		return fresh, fmt.Errorf("订单状态不可发货")
	}
	existing := append([]string{}, order.IssuedCodes...)
	// 重试只补差额。need<=0 时不能回落成 order.Count：已经发满、只是收尾失败的单
	// 会被再发一轮，卡台重复扣费、白号重复占库存，最后按 2×count 发货。
	need := order.Count - len(existing)
	if need < 0 {
		need = 0
	}
	idem := "fulfill-" + order.OrderNo
	if len(existing) > 0 {
		idem = idem + "-retry"
	}

	var newCodes []string
	if need > 0 {
		if db.IsLocalStockPlan(order.Plan) {
			newCodes, err = db.ClaimUnassignedLocalStock(order.AgentUserID, order.Plan, need)
		} else {
			newCodes, err = issueCDKsForAgentOrder(ctx, order.Plan, need, idem)
		}
		if err != nil {
			_ = db.FailAgentOrderDelivery(order.OrderNo, len(existing), existing, err.Error())
			order.FailReason = err.Error()
			order.Status = db.AgentOrderStatusPaidUndelivered
			order.IssuedCodes = existing
			order.IssuedCount = len(existing)
			enqueueAgentOrderFulfillFailed(order)
			return order, err
		}
	}
	allCodes := append(existing, newCodes...)
	if !db.IsLocalStockPlan(order.Plan) {
		assigned, skipped, assignErr := db.AssignCDKsToAgent(order.AgentUserID, allCodes)
		if assignErr != nil {
			_ = db.FailAgentOrderDelivery(order.OrderNo, len(allCodes), allCodes, assignErr.Error())
			order.FailReason = assignErr.Error()
			order.Status = db.AgentOrderStatusPaidUndelivered
			order.IssuedCodes = allCodes
			order.IssuedCount = len(allCodes)
			enqueueAgentOrderFulfillFailed(order)
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
				enqueueAgentOrderFulfillFailed(order)
				return order, fmt.Errorf("%s", msg)
			}
			log.Printf("[agent-order] partial assign order=%s assigned=%d skipped=%v", order.OrderNo, assigned, skipped)
		}
	}

	if len(allCodes) < order.Count {
		reason := fmt.Sprintf("仅发出 %d/%d 张", len(allCodes), order.Count)
		_ = db.FailAgentOrderDelivery(order.OrderNo, len(allCodes), allCodes, reason)
		order.FailReason = reason
		order.Status = db.AgentOrderStatusPaidUndelivered
		order.IssuedCodes = allCodes
		order.IssuedCount = len(allCodes)
		enqueueAgentOrderFulfillFailed(order)
		return order, fmt.Errorf("%s", reason)
	}

	if err := db.CompleteAgentOrderDelivery(order.OrderNo, allCodes); err != nil {
		_ = db.FailAgentOrderDelivery(order.OrderNo, len(allCodes), allCodes, err.Error())
		order.FailReason = err.Error()
		order.Status = db.AgentOrderStatusPaidUndelivered
		order.IssuedCodes = allCodes
		order.IssuedCount = len(allCodes)
		enqueueAgentOrderFulfillFailed(order)
		return order, err
	}
	if fresh, err := db.GetAgentOrderByNo(order.OrderNo); err == nil {
		order = fresh
	} else {
		order.Status = db.AgentOrderStatusDelivered
		order.IssuedCodes = allCodes
		order.IssuedCount = len(allCodes)
		order.FailReason = ""
	}
	enqueueAgentOrderDelivered(order)
	return order, nil
}
