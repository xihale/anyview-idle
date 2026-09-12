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

var ErrKicked = errors.New("token 失效或账号被他处登录顶掉 (WS type=119), 请重新登录")

type Timer struct {
	client *Client
	logf   func(format string, args ...any)

	mu     sync.Mutex
	conn   *websocket.Conn
	hbStop chan struct{}

	open   atomic.Bool
	kicked atomic.Bool
	stay   atomic.Bool // 收到 statusType=403
}

func NewTimer(client *Client, logf func(format string, args ...any)) *Timer {
	return &Timer{client: client, logf: logf}
}

func (t *Timer) send(conn *websocket.Conn, typ int, content any) error {
	msg, err := json.Marshal(map[string]any{
		"type":      typ,
		"content":   content,
		"timestamp": fmt.Sprintf("%d", time.Now().UnixMilli()),
		"token":     t.client.Token,
	})
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return conn.WriteMessage(websocket.TextMessage, msg)
}

// Connect 建连并完成握手: 206 问考试安排 + 启动 40s 心跳。
func (t *Timer) Connect(ctx context.Context) error {
	if t.open.Load() {
		return nil
	}
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
	t.mu.Lock()
	t.conn = conn
	t.mu.Unlock()
	t.open.Store(true)
	t.stay.Store(false)

	go t.readPump(conn)
	t.hbStop = make(chan struct{})
	go t.heartbeat()

	if err := t.send(conn, 206, nil); err != nil {
		return err
	}
	return nil
}

func (t *Timer) readPump(conn *websocket.Conn) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.open.Store(false)
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
		}
	}
}

func (t *Timer) heartbeat() {
	ticker := time.NewTicker(40 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-t.hbStop:
			return
		case <-ticker.C:
			t.mu.Lock()
			conn := t.conn
			t.mu.Unlock()
			if conn == nil || !t.open.Load() {
				continue
			}
			if err := t.send(conn, 117, nil); err != nil {
				t.open.Store(false)
			}
		}
	}
}

func (t *Timer) IsOpen() bool   { return t.open.Load() }
func (t *Timer) IsKicked() bool { return t.kicked.Load() }

func (t *Timer) StartTiming(eid int64) error {
	t.mu.Lock()
	conn := t.conn
	t.mu.Unlock()
	if conn == nil {
		return errors.New("WS 未连接")
	}
	return t.send(conn, 401, eid)
}

func (t *Timer) StopTiming() error {
	t.mu.Lock()
	conn := t.conn
	t.mu.Unlock()
	if conn == nil {
		return nil
	}
	return t.send(conn, 401, nil)
}

func (t *Timer) Close() {
	if t.hbStop != nil {
		select {
		case <-t.hbStop:
		default:
			close(t.hbStop)
		}
	}
	t.mu.Lock()
	conn := t.conn
	t.conn = nil
	t.mu.Unlock()
	t.open.Store(false)
	if conn != nil {
		_ = conn.Close()
	}
}

// Hold 给单题挂 minutes 分钟: 401 开始 → 20s 分片睡眠 → 断线/403 重连续挂剩余 → 401 停止。
// 计时器单活: 服务端同一时刻只记一题, 外层必须逐题串行调用。
func (t *Timer) Hold(ctx context.Context, eid int64, minutes float64) error {
	if t.kicked.Load() {
		return ErrKicked
	}
	if !t.open.Load() {
		if err := t.Connect(ctx); err != nil {
			return err
		}
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
		select {
		case <-ctx.Done():
			_ = t.StopTiming()
			t.Close()
			return ctx.Err()
		case <-time.After(step):
		}
		remain -= step
		if remain <= 0 {
			break
		}
		if t.kicked.Load() {
			return ErrKicked
		}
		// 断线或服务端要求重连(403) → 仿前端: 断开重连后重发 401 续计时
		if !t.open.Load() || t.stay.Load() {
			wasStay := t.stay.Load()
			t.Close()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(3 * time.Second):
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
		}
	}
	if err := t.StopTiming(); err != nil {
		t.logf("  ⚠ 停止计时消息发送失败: %v", err)
	}
	time.Sleep(2 * time.Second)
	t.Close()
	return nil
}
