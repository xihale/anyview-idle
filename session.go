// 多账号会话层: 每个浏览器一条 cookie (aid_sid) 对应一个独立会话——
// 各自的登录态、任务、状态、自动重登计时器互相不可见, 账号可并行刷课;
// 上游请求共享 -rpm 全局限速 (internal/anyview), 出口 IP 压力可控。
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"anyview-idle/internal/anyview"
)

const (
	cookieName  = "aid_sid"
	cookieMaxAge = 180 * 24 * 3600 // 浏览器重启后会话仍在 (挂机场景跨天访问)
	maxSessions  = 32
)

// session = 一个浏览器会话, 承载一个 anyview 账号。
type session struct {
	id    string
	mgr   *anyview.Manager
	timer *time.Timer // 自动重登续跑计时器
	state AppState
}

// newSession 统一的构造入口: 挂上任务结束自动下线回调。
func newSession(id string) *session {
	s := &session{id: id, mgr: anyview.NewManager()}
	s.mgr.SetOnFinish(func(st anyview.RunStatus) { s.finishLogout(st.GainedSec, st.Error) })
	return s
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*session
	file     string
}

var store *sessionStore

type persistedSession struct {
	SID   string   `json:"sid"`
	State AppState `json:"state"`
}

type persistedState struct {
	Sessions []persistedSession `json:"sessions"`
}

func newSessionStore(file string) *sessionStore {
	return &sessionStore{sessions: map[string]*session{}, file: file}
}

func (st *sessionStore) saveLocked() {
	ps := make([]persistedSession, 0, len(st.sessions))
	for _, s := range st.sessions {
		ps = append(ps, persistedSession{SID: s.id, State: s.state})
	}
	b, err := json.MarshalIndent(persistedState{Sessions: ps}, "", "  ")
	if err != nil {
		return
	}
	tmp := st.file + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return
	}
	if err := os.Rename(tmp, st.file); err != nil {
		_ = os.Remove(tmp)
	}
}

func (st *sessionStore) load() {
	st.mu.Lock()
	defer st.mu.Unlock()
	b, err := os.ReadFile(st.file)
	if err != nil {
		return
	}
	var p persistedState
	if err := json.Unmarshal(b, &p); err != nil {
		return // 旧版单账号格式: 放弃旧数据, 从空开始
	}
	for _, e := range p.Sessions {
		if e.SID == "" {
			continue
		}
		st.sessions[e.SID] = newSession(e.SID)
		st.sessions[e.SID].state = e.State
	}
}

func (st *sessionStore) get(sid string) *session {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.sessions[sid]
}

func (st *sessionStore) create(sid string) (*session, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.sessions[sid]; ok {
		return st.sessions[sid], nil
	}
	if len(st.sessions) >= maxSessions {
		return nil, errors.New("会话数已达上限")
	}
	s := newSession(sid)
	st.sessions[sid] = s
	return s, nil
}

func (st *sessionStore) remove(id string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.sessions, id)
	st.saveLocked()
}

func (st *sessionStore) all() []*session {
	st.mu.Lock()
	defer st.mu.Unlock()
	all := make([]*session, 0, len(st.sessions))
	for _, s := range st.sessions {
		all = append(all, s)
	}
	return all
}

func newSID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// sessionFromCookie 按 cookie 找已有会话; 找不到 (没 cookie/未知 sid) 返回 nil。
func sessionFromCookie(r *http.Request) *session {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	return store.get(c.Value)
}

// ensureSession 取已有会话, 没有则建新会话并下发 cookie。
func ensureSession(w http.ResponseWriter, r *http.Request) (*session, error) {
	if s := sessionFromCookie(r); s != nil {
		return s, nil
	}
	s, err := store.create(newSID())
	if err != nil {
		return nil, err
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: s.id, Path: "/",
		MaxAge: cookieMaxAge, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: true,
	})
	return s, nil
}

func stopResumeTimer(s *session) {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
}

// planResume 安排一次延迟重登续跑 (不落盘, 由调用方 saveState); 调用方持有 store.mu。
func planResume(s *session, d time.Duration) {
	t := time.Now().Add(d)
	s.state.ResumeAt = &t
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(d, func() { tryResume(s) })
}

// tryResume 到点重登并续跑。用户期间已手动登录/开任务则放弃本次安排;
// 密码被拒则终止计划 (防锁号); 网络/服务端问题 reloginRetry 后再试。
func tryResume(s *session) {
	store.mu.Lock()
	if s.state.ResumeAt == nil || s.state.TaskOptions == nil || s.state.TaskRunning {
		store.mu.Unlock()
		return
	}
	if s.state.Username == "" || s.state.Password == "" {
		s.state.ResumeAt = nil
		stopResumeTimer(s)
		store.saveLocked()
		store.mu.Unlock()
		log.Printf("[%s] 无保存的账号密码, 无法自动重登, 请手动登录后重新开刷", s.label())
		return
	}
	opts := *s.state.TaskOptions
	user, pass, xff := s.state.Username, s.state.Password, s.state.XFF
	store.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := anyview.NewClient(xff, "")
	student, err := client.Login(ctx, user, pass)
	if err == nil {
		_, err = client.Courses(ctx)
	}
	if err != nil {
		store.mu.Lock()
		if isLoginRejected(err) {
			s.state.ResumeAt = nil
			stopResumeTimer(s)
			store.saveLocked()
			store.mu.Unlock()
			log.Printf("[%s] 自动重登被拒 (%v), 已取消续跑计划, 请手动登录核实密码", s.label(), err)
			return
		}
		planResume(s, reloginRetry)
		store.saveLocked()
		store.mu.Unlock()
		log.Printf("[%s] 自动重登失败 (%v), %v 后再试", s.label(), err, reloginRetry)
		return
	}

	s.mgr.SetSession(client, student)
	store.mu.Lock()
	// 等待期间用户可能已手动登录接管 (ResumeAt 被清)
	if s.state.ResumeAt == nil {
		store.mu.Unlock()
		return
	}
	s.state.ResumeAt = nil
	s.state.Token = client.Token
	s.state.Student = student
	s.state.TaskRunning = true
	store.saveLocked()
	store.mu.Unlock()
	if err := s.mgr.Start(opts); err != nil {
		store.mu.Lock()
		s.state.TaskRunning = false
		store.saveLocked()
		store.mu.Unlock()
		log.Printf("[%s] 续跑启动失败: %v", s.label(), err)
		return
	}
	log.Printf("[%s] 已自动重登 (%s) 并续跑: scheme=%d class=%d [%.1f, %.1f]m",
		s.label(), student.Name, opts.SchemeID, opts.ClassID, opts.LoMin, opts.HiMin)
}

// label 日志前缀: 登录后用学生名, 未登录用 sid 前 8 位。
func (s *session) label() string {
	store.mu.Lock()
	defer store.mu.Unlock()
	if s.state.Student != nil && s.state.Student.Name != "" {
		return s.state.Student.Name
	}
	if len(s.id) >= 8 {
		return s.id[:8]
	}
	return s.id
}

// finishLogout 任务结束后自动下线: 清除会话与持久化 token, 不再占号。
// 任务出错中止 (token 失效/被踢/网络等) 时保留任务并安排延迟重登续跑——
// 用户优先: 先下线让位, 不立刻抢回会话。收尾瞬间用户已开出新一轮时跳过。
func (s *session) finishLogout(gainedSec int64, errMsg string) {
	if s.mgr.Status().Running {
		return
	}
	s.mgr.SetSession(nil, nil)
	store.mu.Lock()
	resume := errMsg != "" && s.state.TaskOptions != nil
	s.state.TaskRunning = false
	s.state.Token = ""
	s.state.Student = nil
	if resume {
		planResume(s, *reloginWait)
	} else {
		s.state.TaskOptions = nil
		s.state.ResumeAt = nil
		stopResumeTimer(s)
	}
	store.saveLocked()
	store.mu.Unlock()
	if resume {
		log.Printf("[%s] 任务出错中止 (%s), 已下线让位; %v 后自动重登续跑", s.label(), errMsg, *reloginWait)
	} else {
		log.Printf("[%s] 任务结束, 已自动下线并清除本地会话 (本轮挂机 +%.1f 分钟)", s.label(), float64(gainedSec)/60)
	}
}

// restore 按旧 initSessionAndTask 语义恢复单个会话: 只为未完成的任务自动上线,
// 平时重启保持离线, 不占账号。
func (s *session) restore() {
	store.mu.Lock()
	st := s.state
	store.mu.Unlock()

	if st.Token == "" && (st.Username == "" || st.Password == "") {
		return
	}
	if st.TaskOptions == nil || (!st.TaskRunning && st.ResumeAt == nil) {
		return
	}

	client := anyview.NewClient(st.XFF, "")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1. 既有 token 还活着就直接续跑 (复用自己的会话不踢人, 无需让位)
	if st.Token != "" {
		client.UseToken(st.Token)
		if _, err := client.Courses(ctx); err == nil {
			student := st.Student
			if student == nil {
				student = &anyview.Student{Name: "token 用户"}
			}
			s.mgr.SetSession(client, student)
			log.Printf("[%s] 已有 Token 仍有效, 直接续跑未完成任务: 方案=%d 班级=%d [%.1f, %.1f]m",
				s.label(), st.TaskOptions.SchemeID, st.TaskOptions.ClassID, st.TaskOptions.LoMin, st.TaskOptions.HiMin)
			store.mu.Lock()
			s.state.ResumeAt = nil
			s.state.TaskRunning = true
			store.saveLocked()
			store.mu.Unlock()
			if err := s.mgr.Start(*st.TaskOptions); err != nil {
				store.mu.Lock()
				s.state.TaskRunning = false
				store.saveLocked()
				store.mu.Unlock()
				log.Printf("[%s] 续跑启动失败: %v", s.label(), err)
			}
			return
		} else {
			log.Printf("[%s] 已有 Token 已失效 (%v)", s.label(), err)
		}
	}

	// 2. token 不可用 → 用户优先: 不立刻重登抢会话, 让位后再自动重登续跑
	lb := s.label()
	store.mu.Lock()
	defer store.mu.Unlock()
	// 旧进程的"在跑"标记已失效, 待办由 ResumeAt 表达; 不清掉会被 tryResume 的守卫拒掉
	s.state.TaskRunning = false
	if st.ResumeAt != nil && st.ResumeAt.After(time.Now()) {
		planResume(s, time.Until(*st.ResumeAt))
		log.Printf("[%s] 维持原有安排: %v 后自动重登续跑", lb, time.Until(*st.ResumeAt).Round(time.Minute))
	} else {
		planResume(s, *reloginWait)
		log.Printf("[%s] token 失效, 为不抢占你正在用的会话, %v 后自动重登续跑", lb, *reloginWait)
	}
	store.saveLocked()
}
