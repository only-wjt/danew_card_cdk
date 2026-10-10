package notify

import (
	"context"
	"log"
	"time"

	"github.com/danew/cdk-recharge-system/internal/db"
)

const (
	pollInterval    = time.Second
	deliveryTimeout = 15 * time.Second
	deliveryLease   = time.Minute
	maxDrain        = 32
)

// Start begins a context-owned worker. Due rows are drained immediately on
// startup, including expired leases from a previous process. Delivery is at
// least once: a crash after Telegram accepts but before the ack can duplicate.
func Start(ctx context.Context) {
	go runWorker(ctx)
}

func runWorker(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		drain(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func retryDelay(attempts int) time.Duration {
	delay := 5 * time.Second
	for i := 1; i < attempts && delay < time.Hour; i++ {
		delay *= 2
	}
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}

func drain(ctx context.Context) {
	for i := 0; i < maxDrain && ctx.Err() == nil; i++ {
		n, err := db.ClaimTelegramNotification(time.Now(), deliveryLease)
		if err != nil {
			log.Printf("[telegram] queue claim failed")
			return
		}
		if n == nil {
			return
		}
		// The context may have been canceled while the claim was in progress.
		// Leave that lease to recover; do not claim another row or start HTTP.
		if ctx.Err() != nil {
			return
		}
		sendCtx, cancel := context.WithTimeout(ctx, deliveryTimeout)
		err = sendNow(sendCtx, n.Text)
		cancel()
		if err == nil {
			err = db.MarkTelegramNotificationSent(n.ID, n.LeaseToken, time.Now())
		} else {
			err = db.MarkTelegramNotificationRetry(n.ID, n.LeaseToken,
				time.Now().Add(retryDelay(n.Attempts)), err.Error())
		}
		if err != nil {
			// DB errors may contain message text; do not echo them into logs.
			log.Printf("[telegram] queue acknowledgement failed")
			return
		}
	}
}
