package anyview

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTimerCloseIdempotent(t *testing.T) {
	tm := NewTimer(&Client{}, nil)
	tm.Close()
	tm.Close()
	if tm.IsOpen() {
		t.Fatal("Close 后不应仍标记为 open")
	}
}

func TestSleepCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("已取消的 ctx 应立刻返回 Canceled, got %v", err)
	}
}

func TestSleepCtxZero(t *testing.T) {
	if err := sleepCtx(context.Background(), 0); err != nil {
		t.Fatalf("d<=0 且 ctx 未取消应返回 nil, got %v", err)
	}
}

func TestHoldRejectsKicked(t *testing.T) {
	tm := NewTimer(&Client{Token: "x"}, nil)
	tm.kicked.Store(true)
	err := tm.Hold(context.Background(), 1, 0.01)
	if !errors.Is(err, ErrKicked) {
		t.Fatalf("kicked 应返回 ErrKicked, got %v", err)
	}
	if tm.IsOpen() {
		t.Fatal("Hold 返回后连接应已关闭")
	}
}
