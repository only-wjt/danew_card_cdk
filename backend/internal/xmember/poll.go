package xmember

import (
	"context"
	"log"
	"time"
)

// Start 每 15 秒推进一次到期的 X 兑换。没有 webhook，结果只能靠查。
func Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("[x-member] poll panic: %v", r)
						}
					}()
					PollDue(ctx)
				}()
			}
		}
	}()
}
