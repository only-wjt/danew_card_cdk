// Package plansync 每3分钟从卡台同步逻辑套餐状态和实体产品列表。
package plansync

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/danew/cdk-recharge-system/internal/cardplatform"
	"github.com/danew/cdk-recharge-system/internal/db"
)

const syncInterval = 3 * time.Minute

// SyncResult 同步结果摘要。
type SyncResult struct {
	Plans    int
	Products int
}

// Start 启动后台产品状态同步（goroutine；ctx.Done() 时优雅退出）。
func Start(ctx context.Context) {
	go run(ctx)
}

func run(ctx context.Context) {
	// 启动时立即同步一次
	syncOnce(ctx)
	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("[plan-sync] stopped")
			return
		case <-ticker.C:
			syncOnce(ctx)
		}
	}
}

// SyncNow 供 handler 主动触发（同步调用，有 ctx 超时保护）。
func SyncNow(ctx context.Context) (SyncResult, error) {
	ctx2, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return doSync(ctx2)
}

// SyncNowForAccount 只同步指定卡台账户，A/B 的产品编码和在线状态互不覆盖。
func SyncNowForAccount(ctx context.Context, accountID int64) (SyncResult, error) {
	ctx2, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	accounts, err := db.ListCardPlatformAccounts()
	if err != nil {
		return SyncResult{}, err
	}
	for _, acc := range accounts {
		if acc.ID == accountID {
			return doSyncAccount(ctx2, acc)
		}
	}
	return SyncResult{}, fmt.Errorf("card platform account %d not found", accountID)
}

func syncOnce(ctx context.Context) {
	ctx2, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r, err := doSync(ctx2)
	if err != nil {
		log.Printf("[plan-sync] error: %v", err)
		return
	}
	log.Printf("[plan-sync] synced %d plans, %d products", r.Plans, r.Products)
	accounts, aerr := db.ActiveDualIssueAccounts()
	if aerr != nil {
		log.Printf("[plan-sync] list accounts: %v", aerr)
		return
	}
	for _, acc := range accounts {
		ar, err := doSyncAccount(ctx2, acc)
		if err != nil {
			log.Printf("[plan-sync] account=%d error: %v", acc.ID, err)
			continue
		}
		log.Printf("[plan-sync] account=%d synced %d plans, %d products", acc.ID, ar.Plans, ar.Products)
	}
	probes, perr := db.CircuitProbeAccounts()
	if perr != nil {
		log.Printf("[plan-sync] list circuit probes: %v", perr)
		return
	}
	for _, acc := range probes {
		probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, err := doSyncAccount(probeCtx, acc)
		cancel()
		if err != nil {
			log.Printf("[plan-sync] circuit probe account=%d failed: %v", acc.ID, err)
		} else {
			log.Printf("[plan-sync] circuit probe account=%d recovered", acc.ID)
		}
	}
}

func doSyncAccount(ctx context.Context, acc db.CardPlatformAccount) (SyncResult, error) {
	if strings.TrimSpace(acc.CredSecret) == "" {
		err := fmt.Errorf("card platform account %d has no API key", acc.ID)
		_ = db.MarkCardPlatformAccountError(acc.ID, err.Error())
		return SyncResult{}, err
	}
	base := strings.TrimRight(strings.TrimSpace(acc.SiteBase), "/")
	base = strings.TrimSuffix(base, "/openapi/v1")
	base = strings.TrimSuffix(base, "/openapi")
	cli := cardplatform.NewFromAccount(acc)
	var res SyncResult
	plans, err := cli.GetPlans(ctx)
	if err != nil {
		_ = db.MarkCardPlatformAccountError(acc.ID, err.Error())
		return res, err
	}
	// /plans 成功已足以证明卡台恢复；产品接口失败只影响产品缓存，不应让熔断永久打开。
	_ = db.MarkCardPlatformAccountOK(acc.ID)
	for _, p := range plans.SellablePlans() {
		if err := db.UpsertPlanStatusForAccount(acc.ID, p.Key, p.Label, true, p.ServiceFeeUsdMinor); err != nil {
			log.Printf("[plan-sync] account=%d upsert plan %s: %v", acc.ID, p.Key, err)
		} else {
			res.Plans++
		}
	}
	items, present, err := fetchProductsForCache(ctx, cli)
	if err != nil {
		log.Printf("[plan-sync] account=%d products: %v", acc.ID, err)
		return res, nil
	}
	for _, cp := range items {
		cp.AccountID = acc.ID
		if err := db.UpsertCardProductForAccount(acc.ID, cp); err != nil {
			log.Printf("[plan-sync] account=%d upsert product %s: %v", acc.ID, cp.ProductCode, err)
		} else {
			res.Products++
		}
	}
	if _, err := db.MarkCardProductsOfflineExceptForAccount(acc.ID, present); err != nil {
		log.Printf("[plan-sync] account=%d mark offline: %v", acc.ID, err)
	}
	return res, nil
}

func doSync(ctx context.Context) (SyncResult, error) {
	cfg := cardplatform.LoadConfig()
	if cfg.APIKey == "" {
		return SyncResult{}, nil // 未配置 API Key，静默跳过
	}
	cli := cardplatform.New(cfg)
	var res SyncResult

	// 1. 同步逻辑套餐。
	// ★只同步可卖档位★：卡台透传的是 ACC 的整张定价表，里面有 claude_*（本系统
	// 没有兑换流程）。全量写进 plan_status_cache 的话，选卡规则页会冒出一堆
	// 根本配不了卡的档位，运营还得去猜哪些是能用的。
	plans, err := cli.GetPlans(ctx)
	if err != nil {
		return res, err
	}
	for _, p := range plans.SellablePlans() {
		if err := db.UpsertPlanStatus(p.Key, p.Label, true, p.ServiceFeeUsdMinor); err != nil {
			log.Printf("[plan-sync] upsert plan %s: %v", p.Key, err)
		} else {
			res.Plans++
		}
	}

	// 2. 优先 /gpt-direct/card-products（含未启动卡头），失败再回落 /products。
	items, present, err := fetchProductsForCache(ctx, cli)
	if err != nil {
		// 产品接口失败不阻断套餐同步结果，也绝不把全表标下线（避免短暂 5xx 误杀）
		log.Printf("[plan-sync] products error: %v", err)
		return res, nil
	}
	for _, cp := range items {
		if err := db.UpsertCardProduct(cp); err != nil {
			log.Printf("[plan-sync] upsert product %s: %v", cp.ProductCode, err)
		} else {
			res.Products++
		}
	}
	// 3. 本次未返回的历史缓存 → 标已下线（如全部 VISA 已从卡台下架）
	if off, err := db.MarkCardProductsOfflineExcept(present); err != nil {
		log.Printf("[plan-sync] mark offline: %v", err)
	} else if off > 0 {
		log.Printf("[plan-sync] marked %d products offline (not in openable list)", off)
	}
	return res, nil
}

// fetchProductsForCache 优先卡台 card-products（能区分未启动），没有该接口再走 /products。
func fetchProductsForCache(ctx context.Context, cli *cardplatform.Client) ([]db.CardProductCache, map[string]bool, error) {
	if dps, err := cli.GetDirectCardProducts(ctx); err == nil && len(dps) > 0 {
		present := make(map[string]bool, len(dps))
		strict := make([]db.CardProductCache, 0, len(dps))
		loose := make([]db.CardProductCache, 0, len(dps))
		strictOnline := 0
		for _, p := range dps {
			code := strings.TrimSpace(p.ProductCode)
			if code == "" {
				continue
			}
			present[code] = true
			susp := ""
			if p.Suspended {
				susp = strings.TrimSpace(p.SuspendReason)
				if susp == "" {
					susp = "suspended"
				}
			}
			base := db.CardProductCache{
				ProductCode: code,
				Issuer:      p.Issuer,
				BIN:         p.BIN,
				Description: p.Label,
				SuspendedAt: susp,
			}
			looseEnabled := p.Enabled && !p.Suspended
			strictEnabled := looseEnabled && (p.Usable || p.ChannelOpen)
			l, s := base, base
			l.Enabled = looseEnabled
			s.Enabled = strictEnabled
			loose = append(loose, l)
			strict = append(strict, s)
			if strictEnabled {
				strictOnline++
			}
		}
		if strictOnline > 0 {
			return overlayProductDetails(ctx, cli, strict), present, nil
		}
		return overlayProductDetails(ctx, cli, loose), present, nil
	}
	products, err := cli.GetProducts(ctx)
	if err != nil {
		return nil, nil, err
	}
	present := make(map[string]bool, len(products))
	items := make([]db.CardProductCache, 0, len(products))
	for _, p := range products {
		code := strings.TrimSpace(p.ProductCode)
		if code == "" {
			continue
		}
		present[code] = true
		items = append(items, db.CardProductCache{
			ProductCode: code,
			Issuer:      p.Issuer,
			BIN:         p.BIN,
			Network:     p.Network,
			IssuingArea: p.IssuingArea,
			Scene:       p.Scene,
			CardGroup:   p.CardGroup,
			Description: p.Description,
			BinHeads:    p.BinHeads,
			Enabled:     true,
			SuspendedAt: p.SuspendedAt,
		})
	}
	return items, present, nil
}

// overlayProductDetails 用 /products 补 BIN/地区等展示字段；失败则保留 card-products 结果。
func overlayProductDetails(ctx context.Context, cli *cardplatform.Client, items []db.CardProductCache) []db.CardProductCache {
	extras, err := cli.GetProducts(ctx)
	if err != nil || len(extras) == 0 {
		return items
	}
	byCode := make(map[string]cardplatform.ProductInfo, len(extras))
	for _, p := range extras {
		code := strings.TrimSpace(p.ProductCode)
		if code != "" {
			byCode[strings.ToUpper(code)] = p
		}
	}
	for i, item := range items {
		p, ok := byCode[strings.ToUpper(item.ProductCode)]
		if !ok {
			continue
		}
		if item.BIN == "" {
			item.BIN = p.BIN
		}
		item.Network = p.Network
		item.IssuingArea = p.IssuingArea
		item.Scene = p.Scene
		item.CardGroup = p.CardGroup
		if item.Description == "" {
			item.Description = p.Description
		}
		if len(item.BinHeads) == 0 {
			item.BinHeads = p.BinHeads
		}
		items[i] = item
	}
	return items
}
