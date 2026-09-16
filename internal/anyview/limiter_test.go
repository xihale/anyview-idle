package anyview

import (
	"context"
	"testing"
	"time"
)

// 放行间隔: -rpm=120 ⇒ 相邻放行 60s/120=500ms; rpm=0 关闭。
func TestLimiterSpacing(t *testing.T) {
	SetUpstreamRPM(0)
	if sharedLimiter != nil {
		t.Fatal("rpm=0 应关闭限速")
	}

	SetUpstreamRPM(120)
	defer SetUpstreamRPM(0)
	ctx := context.Background()

	start := time.Now()
	if err := sharedLimiter.wait(ctx); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("首次放行不应排队: %v", d)
	}

	start = time.Now()
	if err := sharedLimiter.wait(ctx); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 400*time.Millisecond {
		t.Fatalf("第二次放行应间隔 500ms, 实际 %v", d)
	}
}

// 窗口占满后排队可被 ctx 取消, 不会干等到 20s 上限。
func TestLimiterContextCancel(t *testing.T) {
	SetUpstreamRPM(120)
	defer SetUpstreamRPM(0)
	if err := sharedLimiter.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := sharedLimiter.wait(ctx); err == nil {
		t.Fatal("ctx 已取消应报错")
	} else if d := time.Since(start); d > time.Second {
		t.Fatalf("取消后应立即返回, 实际等了 %v", d)
	}
}
