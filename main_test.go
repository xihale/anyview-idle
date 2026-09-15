package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"anyview-idle/internal/anyview"
)

func resetForTest(t *testing.T) {
	dir := t.TempDir()
	oldFile := *stateFile
	*stateFile = filepath.Join(dir, "state.json")
	t.Cleanup(func() { *stateFile = oldFile })

	oldMgr := mgr
	mgr = anyview.NewManager()
	t.Cleanup(func() { mgr = oldMgr })

	oldWait := *reloginWait
	*reloginWait = time.Minute
	t.Cleanup(func() { *reloginWait = oldWait })

	if resumeTimer != nil {
		resumeTimer.Stop()
		resumeTimer = nil
	}
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

// 正常结束: 清掉会话/token/任务, 不安排续跑; 学号密码保留供中断续跑。
func TestFinishLogoutCleanClearsAll(t *testing.T) {
	resetForTest(t)
	appState = loginState()
	mgr.SetSession(anyview.NewClient("", ""), appState.Student)

	finishLogout(120, "")

	if mgr.Client() != nil {
		t.Fatal("会话未清除")
	}
	if appState.Token != "" || appState.Student != nil || appState.TaskOptions != nil || appState.TaskRunning {
		t.Fatalf("状态未清空: %+v", appState)
	}
	if appState.ResumeAt != nil || resumeTimer != nil {
		t.Fatal("正常结束不应安排续跑")
	}
	if appState.Username == "" || appState.Password == "" {
		t.Fatal("凭据不应被清除 (中断续跑依赖)")
	}
}

// 出错中止 (token 失效/被踢): 仍下线让位, 但保留任务并安排延迟重登续跑。
func TestFinishLogoutErrorSchedulesResume(t *testing.T) {
	resetForTest(t)
	appState = loginState()
	mgr.SetSession(anyview.NewClient("", ""), appState.Student)

	finishLogout(60, "token 失效或账号被他处登录顶掉 (WS type=119)")

	if mgr.Client() != nil {
		t.Fatal("会话未清除")
	}
	if appState.Token != "" || appState.Student != nil || appState.TaskRunning {
		t.Fatalf("下线未完成: %+v", appState)
	}
	if appState.TaskOptions == nil {
		t.Fatal("出错中止应保留 TaskOptions 供续跑")
	}
	if appState.ResumeAt == nil || !appState.ResumeAt.After(time.Now()) {
		t.Fatalf("未安排续跑时间: %+v", appState.ResumeAt)
	}
	b, err := os.ReadFile(*stateFile)
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
