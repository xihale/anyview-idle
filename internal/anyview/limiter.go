// 全局上游限速: 多账号并行后所有请求仍从同一个出口 IP 发出, WAF 按 IP 限速拉黑,
// 故所有账号共享一个滑动窗口 (语义与 anyview-next relay 的 limiter 对齐):
// 滚动 60s 内最多放行 perMin 个请求, 相邻放行间隔 window/perMin 均匀铺开。
package anyview

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	upstreamWindow  = 60 * time.Second
	upstreamMaxWait = 20 * time.Second // 须 < Client.Timeout(30s), 否则排队会被连接超时吞掉
)

type upstreamLimiter struct {
	mu     sync.Mutex
	perMin int
	passed []time.Time // 窗口内已放行时刻, 升序
}

var sharedLimiter *upstreamLimiter

// SetUpstreamRPM 设置全局上游限速 (每分钟放行数, 所有账号共享); n<=0 关闭。
// 须在发起上游请求前调用一次。
func SetUpstreamRPM(n int) {
	if n <= 0 {
		sharedLimiter = nil
		return
	}
	sharedLimiter = &upstreamLimiter{perMin: n}
}

// wait 拿一个放行名额, 窗口占满时排队; 排队总时长超 upstreamMaxWait 或 ctx 取消则报错。
func (l *upstreamLimiter) wait(ctx context.Context) error {
	deadline := time.Now().Add(upstreamMaxWait)
	for {
		l.mu.Lock()
		now := time.Now()
		for len(l.passed) > 0 && now.Sub(l.passed[0]) >= upstreamWindow {
			l.passed = l.passed[1:]
		}
		var hold time.Duration
		if len(l.passed) < l.perMin {
			var earliest time.Time
			if n := len(l.passed); n > 0 {
				earliest = l.passed[n-1].Add(upstreamWindow / time.Duration(l.perMin))
			}
			if now.After(earliest) {
				l.passed = append(l.passed, now)
				l.mu.Unlock()
				return nil
			}
			hold = earliest.Sub(now)
		} else {
			hold = upstreamWindow - now.Sub(l.passed[0])
		}
		perMin := l.perMin
		l.mu.Unlock()

		if now.Add(hold).After(deadline) {
			return fmt.Errorf("上游限速: 60s 窗口已满(-rpm=%d), 排队超过 %v 放弃", perMin, upstreamMaxWait)
		}
		if err := sleepCtx(ctx, hold); err != nil {
			return err
		}
	}
}
