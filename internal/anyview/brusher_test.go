package anyview

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBrushOptionsValidate(t *testing.T) {
	ok := BrushOptions{SchemeID: 1, ClassID: 2, LoMin: 5, HiMin: 10, InjectLo: 0, InjectHi: 3}
	if err := ok.validate(); err != nil {
		t.Fatalf("合法区间应通过: %v", err)
	}

	cases := []BrushOptions{
		{ClassID: 2, LoMin: 5, HiMin: 10},               // 缺 scheme
		{SchemeID: 1, LoMin: 5, HiMin: 10},              // 缺 class
		{SchemeID: 1, ClassID: 2, LoMin: 0, HiMin: 10},  // lo=0
		{SchemeID: 1, ClassID: 2, LoMin: 10, HiMin: 5},  // lo>hi
		{SchemeID: 1, ClassID: 2, LoMin: 1, HiMin: 181}, // hi 过大 → Duration 溢出风险
		{SchemeID: 1, ClassID: 2, LoMin: 1, HiMin: 2, InjectLo: 3, InjectHi: 1},
		{SchemeID: 1, ClassID: 2, LoMin: 1, HiMin: 2, Offset: -1},
		{SchemeID: 1, ClassID: 2, LoMin: 1, HiMin: 2, Limit: -1},
	}
	for i, o := range cases {
		if err := o.validate(); err == nil {
			t.Errorf("case %d 应拒绝: %+v", i, o)
		}
	}
}

func TestClipCatalog(t *testing.T) {
	items := make([]CatalogItem, 10)
	for i := range items {
		items[i].EID = int64(i + 1)
	}

	got := clipCatalog(items, 0, 0)
	if len(got) != 10 {
		t.Fatalf("limit=0 应保留全部, got %d", len(got))
	}

	got = clipCatalog(items, 3, 4)
	if len(got) != 4 || got[0].EID != 4 || got[3].EID != 7 {
		t.Fatalf("offset=3 limit=4 切片错: %+v", got)
	}

	got = clipCatalog(items, 10, 5)
	if got != nil {
		t.Fatalf("offset==len 应为空, got %+v", got)
	}
	got = clipCatalog(items, 99, 5)
	if got != nil {
		t.Fatalf("offset>len 应为空, got %+v", got)
	}

	got = clipCatalog(items, 8, 10)
	if len(got) != 2 || got[0].EID != 9 {
		t.Fatalf("limit 超出剩余应截到末尾: %+v", got)
	}

	got = clipCatalog(nil, 0, 5)
	if got != nil {
		t.Fatalf("空目录应为空, got %+v", got)
	}
}

func TestFinishDoesNotFireIfSuperseded(t *testing.T) {
	m := NewManager()
	fired := make(chan struct{}, 1)
	m.SetOnFinish(func(RunStatus) { fired <- struct{}{} })

	ctx, cancel := context.WithCancel(context.Background())
	r := &run{ctx: ctx, cancel: cancel, mgr: m}
	m.mu.Lock()
	m.run = r
	m.mu.Unlock()

	m.mu.Lock()
	m.run = nil // SetSession 顶掉
	m.mu.Unlock()
	r.finish(nil)

	select {
	case <-fired:
		t.Fatal("被顶掉的 run 不应触发 onFinish")
	case <-time.After(80 * time.Millisecond):
	}
}

func TestFinishFiresIfCurrent(t *testing.T) {
	m := NewManager()
	fired := make(chan RunStatus, 1)
	m.SetOnFinish(func(st RunStatus) { fired <- st })

	ctx, cancel := context.WithCancel(context.Background())
	r := &run{ctx: ctx, cancel: cancel, mgr: m}
	r.logf("hello")
	m.mu.Lock()
	m.run = r
	m.mu.Unlock()
	r.finish(nil)

	select {
	case st := <-fired:
		if st.Running {
			t.Fatal("finish 后 Running 应为 false")
		}
		if len(st.Log) != 1 || !strings.Contains(st.Log[0], "hello") {
			t.Fatalf("onFinish 应带上日志, got %+v", st.Log)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("当前 run 应触发 onFinish")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.run != nil {
		t.Fatal("finish 后 mgr.run 应清空")
	}
}
