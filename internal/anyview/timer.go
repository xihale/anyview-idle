// exam WebSocket 计时器, 协议同前端 websocket-exam.ts (analysis/10):
//
//	wss://…/api/exam-service/websocket/{token}
//	{type:206, content:null}  连上先问考试安排 (前端行为, 回 212=无考试)
//	{type:117, content:null}  心跳, 前端每 40s 一次
//	{type:401, content:<eID>} 开始给该题计时; content=null 停止
//	type:119 = token 失效/单点登录被踢; statusType:403 = 停留过长, 前端做法是断开重连后重发 401 续计时
//
// 每条消息带 timestamp(毫秒字符串) 和 token 字段。
package anyview

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

var (
	ErrKicked        = errors.New("token 失效或账号被他处登录顶掉 (WS type=119), 请重新登录")
	errNeedReconnect = errors.New("ws need reconnect")
)

const wsWriteTimeout = 10 * time.Second

type Timer struct {
	client *Client
	logf   func(format string, args ...any)

	mu       sync.Mutex
	writeMu  sync.Mutex
	conn     *websocket.Conn
	hbCancel context.CancelFunc

	notify chan struct{} // 1-buffer, never closed; readPump/403/断线唤醒 Hold

	open   atomic.Bool
	kicked atomic.Bool
	stay   atomic.Bool // 收到 statusType=403
}

func NewTimer(client *Client, logf func(format string, args ...any)) *Timer {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Timer{client: client, logf: logf, notify: make(chan struct{}, 1)}
}

func (t *Timer) signal() {
	select {
	case t.notify <- struct{}{}:
	default:
	}
}

func (t *Timer) drainSignals() {
	for {
		select {
		case <-t.notify:
		default:
			return
		}
	}
}

func (t *Timer) sendConn(conn *websocket.Conn, typ int, content any) error {
	if conn == nil {
		return errors.New("WS 未连接")
	}
	msg, err := json.Marshal(map[string]any{
		"type":      typ,
		"content":   content,
		"timestamp": fmt.Sprintf("%d", time.Now().UnixMilli()),
		"token":     t.client.Token,
	})
	if err != nil {
		return err
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
	return conn.WriteMessage(websocket.TextMessage, msg)
}

func (t *Timer) currentConn() *websocket.Conn {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.conn
}

// Connect 建连并完成握手: 206 问考试安排 + 启动 40s 心跳。
// 半死连接 (readPump 已退出但心跳还在) 会先 Close 再重拨, 避免双心跳。
func (t *Timer) Connect(ctx context.Context) error {
	if t.open.Load() && t.currentConn() != nil {
		return nil
	}
	t.Close()

	header := http.Header{}
	header.Set("User-Agent", t.client.UA)
	header.Set("Origin", BaseURL)
	if t.client.XFF != "" {
		header.Set("X-Forwarded-For", t.client.XFF)
	}
	dialer := websocket.Dialer{
		Jar:              t.client.Jar, // 带上 WAF cookie, 同浏览器行为
		TLSClientConfig:  &tls.Config{},
		HandshakeTimeout: 15 * time.Second,
	}
	url := fmt.Sprintf("%s/%s", WSBaseURL, t.client.Token)
	conn, _, err := dialer.DialContext(ctx, url, header)
	if err != nil {
		return fmt.Errorf("WS 握手失败: %w", err)
	}

	hbCtx, hbCancel := context.WithCancel(context.Background())
	t.mu.Lock()
	t.conn = conn
	t.hbCancel = hbCancel
	t.mu.Unlock()
	t.open.Store(true)
	t.stay.Store(false)
	t.drainSignals()

	go t.readPump(conn)
	go t.heartbeat(hbCtx, conn)

	if err := t.sendConn(conn, 206, nil); err != nil {
		t.Close()
		return err
	}
	return nil
}

func (t *Timer) noteDead(conn *websocket.Conn) {
	t.mu.Lock()
	if t.conn == conn {
		t.open.Store(false)
	}
	t.mu.Unlock()
	t.signal()
}

func (t *Timer) readPump(conn *websocket.Conn) {
	defer t.noteDead(conn)
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg struct {
			Type       *int `json:"type"`
			StatusType *int `json:"statusType"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue // 忽略非 JSON 帧
		}
		if msg.Type != nil && *msg.Type == 119 {
			t.kicked.Store(true)
			t.logf("⚠ WS type=119: token 失效或账号被他处登录顶掉")
			return
		}
		if msg.StatusType != nil && *msg.StatusType == 403 {
			t.stay.Store(true)
			t.logf("⚠ 服务端提示停留过长 (statusType=403), 将断开重连续挂")
			t.signal()
		}
	}
}

func (t *Timer) heartbeat(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(40 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := t.sendConn(conn, 117, nil); err != nil {
				t.noteDead(conn)
				return
			}
		}
	}
}

func (t *Timer) IsOpen() bool   { return t.open.Load() }
func (t *Timer) IsKicked() bool { return t.kicked.Load() }

func (t *Timer) StartTiming(eid int64) error {
	conn := t.currentConn()
	if conn == nil {
		return errors.New("WS 未连接")
	}
	return t.sendConn(conn, 401, eid)
}

func (t *Timer) StopTiming() error {
	conn := t.currentConn()
	if conn == nil {
		return nil
	}
	return t.sendConn(conn, 401, nil)
}

func (t *Timer) Close() {
	t.mu.Lock()
	hbCancel := t.hbCancel
	t.hbCancel = nil
	conn := t.conn
	t.conn = nil
	t.mu.Unlock()
	t.open.Store(false)
	if hbCancel != nil {
		hbCancel()
	}
	if conn != nil {
		_ = conn.Close()
	}
}

func (t *Timer) waitSlice(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		case <-t.notify:
			if t.kicked.Load() {
				return ErrKicked
			}
			if !t.open.Load() || t.stay.Load() {
				return errNeedReconnect
			}
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}

// Hold 给单题挂 minutes 分钟: 401 开始 → 分片睡眠 → 断线/403 立刻重连续挂剩余 → 401 停止。
// 计时器单活: 服务端同一时刻只记一题, 外层必须逐题串行调用。
// 任意返回路径都会 StopTiming+Close, 避免踢号/取消后连接与心跳泄漏。
func (t *Timer) Hold(ctx context.Context, eid int64, minutes float64) (err error) {
	if minutes < 0 {
		minutes = 0
	}
	defer func() {
		if !errors.Is(err, ErrKicked) {
			_ = t.StopTiming()
		}
		t.Close()
	}()

	if t.kicked.Load() {
		return ErrKicked
	}
	if err := t.Connect(ctx); err != nil {
		return err
	}
	if err := t.StartTiming(eid); err != nil {
		return err
	}

	remain := time.Duration(minutes * float64(time.Minute))
	const slice = 20 * time.Second
	for remain > 0 {
		step := slice
		if remain < step {
			step = remain
		}
		started := time.Now()
		waitErr := t.waitSlice(ctx, step)
		elapsed := time.Since(started)
		if elapsed >= remain {
			remain = 0
		} else {
			remain -= elapsed
		}

		switch {
		case waitErr == nil:
			continue
		case errors.Is(waitErr, context.Canceled), errors.Is(waitErr, context.DeadlineExceeded):
			return waitErr
		case errors.Is(waitErr, ErrKicked):
			return ErrKicked
		case errors.Is(waitErr, errNeedReconnect):
			if remain <= 0 {
				break
			}
			if t.kicked.Load() {
				return ErrKicked
			}
			wasStay := t.stay.Load()
			t.Close()
			if err := sleepCtx(ctx, 3*time.Second); err != nil {
				return err
			}
			if err := t.Connect(ctx); err != nil {
				return err
			}
			if err := t.StartTiming(eid); err != nil {
				return err
			}
			if wasStay {
				t.logf("  ↻ 已重连并续挂剩余 %.1f 分钟", remain.Minutes())
			} else {
				t.logf("  ⚠ WS 断开, 重连续挂剩余 %.1f 分钟", remain.Minutes())
			}
		default:
			return waitErr
		}
	}
	_ = t.StopTiming()
	if err := sleepCtx(ctx, 2*time.Second); err != nil {
		return err
	}
	return nil
}
