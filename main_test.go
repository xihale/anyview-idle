package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"anyview-idle/internal/anyview"
)

// newTestSession 建一个独立临时 store 并返回其中一个会话。
func newTestSession(t *testing.T, sid string) *session {
	t.Helper()
	store = newSessionStore(filepath.Join(t.TempDir(), "state.json"))
	t.Cleanup(func() { store = nil })

	oldWait := *reloginWait
	*reloginWait = time.Minute
	t.Cleanup(func() { *reloginWait = oldWait })

	s, err := store.create(sid)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		store.mu.Lock()
		stopResumeTimer(s)
		store.mu.Unlock()
	})
	return s
}

func loginState() AppState {
	return AppState{
		Username:    "3125004109",
		Password:    "pw",
		Token:       "tok",
		Student:     &anyview.Student{Name: "韩亮"},
		TaskRunning: true,
		TaskOptions: &anyview.BrushOptions{SchemeID: 465, ClassID: 381, LoMin: 5, HiMin: 10},
	}
}

func statusBody(t *testing.T, sid string) map[string]any {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/status", nil)
	if sid != "" {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: sid})
	}
	w := httptest.NewRecorder()
	handleStatus(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: HTTP %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("status 响应非 JSON: %v", err)
	}
	return body
}

// 正常结束: 清掉会话/token/任务, 不安排续跑; 学号密码保留供中断续跑。
func TestFinishLogoutCleanClearsAll(t *testing.T) {
	s := newTestSession(t, "sess-clean-0001")
	s.state = loginState()
	s.mgr.SetSession(anyview.NewClient("", ""), s.state.Student)

	s.finishLogout(120, "")

	if s.mgr.Client() != nil {
		t.Fatal("会话未清除")
	}
	if s.state.Token != "" || s.state.Student != nil || s.state.TaskOptions != nil || s.state.TaskRunning {
		t.Fatalf("状态未清空: %+v", s.state)
	}
	if s.state.ResumeAt != nil || s.timer != nil {
		t.Fatal("正常结束不应安排续跑")
	}
	if s.state.Username == "" || s.state.Password == "" {
		t.Fatal("凭据不应被清除 (中断续跑依赖)")
	}
}

// 出错中止 (token 失效/被踢): 仍下线让位, 但保留任务并安排延迟重登续跑。
func TestFinishLogoutErrorSchedulesResume(t *testing.T) {
	s := newTestSession(t, "sess-err-00002")
	s.state = loginState()
	s.mgr.SetSession(anyview.NewClient("", ""), s.state.Student)

	s.finishLogout(60, "token 失效或账号被他处登录顶掉 (WS type=119)")

	if s.mgr.Client() != nil {
		t.Fatal("会话未清除")
	}
	if s.state.Token != "" || s.state.Student != nil || s.state.TaskRunning {
		t.Fatalf("下线未完成: %+v", s.state)
	}
	if s.state.TaskOptions == nil {
		t.Fatal("出错中止应保留 TaskOptions 供续跑")
	}
	if s.state.ResumeAt == nil || !s.state.ResumeAt.After(time.Now()) {
		t.Fatalf("未安排续跑时间: %+v", s.state.ResumeAt)
	}
	if s.timer == nil {
		t.Fatal("重登计时器未安排")
	}
	b, err := os.ReadFile(store.file)
	if err != nil {
		t.Fatalf("状态文件未落盘: %v", err)
	}
	if strings.Contains(string(b), "\"token\": \"tok\"") {
		t.Fatal("落盘状态仍含旧 token")
	}
	if !strings.Contains(string(b), "resumeAt") {
		t.Fatal("落盘状态缺 resumeAt")
	}
}

func TestIsLoginRejected(t *testing.T) {
	if !isLoginRejected(errors.New("登录失败: 用户名或密码错误")) {
		t.Fatal("密码被拒应判定为拒绝")
	}
	if isLoginRejected(errors.New("登录请求失败: HTTP 403: wzws")) {
		t.Fatal("网络/WAF 问题不应判定为拒绝 (需重试)")
	}
	if isLoginRejected(errors.New("取 RSA 公钥失败: context deadline exceeded")) {
		t.Fatal("网络问题不应判定为拒绝 (需重试)")
	}
}

// 会话隔离: A 登录的账号/token 对 B 不可见; B 未登录时各接口按未登录处理。
func TestSessionsAreIsolated(t *testing.T) {
	a := newTestSession(t, "sess-isol-a03")
	b, err := store.create("sess-isol-b04")
	if err != nil {
		t.Fatal(err)
	}
	a.state = loginState()

	if got := statusBody(t, a.id)["token"]; got != "tok" {
		t.Fatalf("A 应看到自己的 token, 得到 %v", got)
	}
	if got := statusBody(t, b.id)["token"]; got != "" {
		t.Fatalf("B 不应看到 A 的 token, 得到 %v", got)
	}
	if got := statusBody(t, "")["authenticated"]; got != false {
		t.Fatalf("无 cookie 应视为未登录, 得到 %v", got)
	}

	r := httptest.NewRequest("GET", "/api/courses", nil)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: b.id})
	w := httptest.NewRecorder()
	handleCourses(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("B 未登录调 courses 应 401, 得到 %d", w.Code)
	}
}

// 持久化: 落盘后重新 load, 会话与账号状态应原样恢复 (含 sid, 浏览器刷新 cookie 仍有效)。
func TestStorePersistenceRoundtrip(t *testing.T) {
	s := newTestSession(t, "sess-disk-0005")
	s.state = loginState()
	store.mu.Lock()
	store.saveLocked()
	store.mu.Unlock()

	reloaded := newSessionStore(store.file)
	reloaded.load()
	got := reloaded.get("sess-disk-0005")
	if got == nil {
		t.Fatal("重载后应找回原 sid 的会话")
	}
	if got.state.Token != "tok" || got.state.Student == nil || got.state.TaskOptions == nil {
		t.Fatalf("账号状态未恢复: %+v", got.state)
	}
}

// 会话数上限: 匿名刷会话不能撑爆内存。
func TestStoreSessionCap(t *testing.T) {
	newTestSession(t, "sess-cap-base")
	var err error
	for i := 0; i < maxSessions; i++ {
		_, err = store.create("cap-" + strconv.Itoa(i))
		if err != nil {
			break
		}
	}
	if err == nil {
		t.Fatal("超过上限的 create 应失败")
	}
}
