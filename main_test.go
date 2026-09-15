package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"anyview-idle/internal/anyview"
)

// finishLogout 必须清掉会话与 token/任务状态, 但保留学号密码供中断续跑。
func TestFinishLogoutClearsSessionKeepsCredentials(t *testing.T) {
	dir := t.TempDir()
	oldFile := *stateFile
	*stateFile = filepath.Join(dir, "state.json")
	defer func() { *stateFile = oldFile }()

	oldMgr := mgr
	mgr = anyview.NewManager()
	defer func() { mgr = oldMgr }()

	appState = AppState{
		Username:    "3125004109",
		Password:    "pw",
		Token:       "tok",
		Student:     &anyview.Student{Name: "韩亮"},
		TaskRunning: true,
		TaskOptions: &anyview.BrushOptions{SchemeID: 465, ClassID: 381},
	}
	mgr.SetSession(anyview.NewClient("", ""), appState.Student)

	finishLogout(120)

	if mgr.Client() != nil {
		t.Fatal("会话未清除")
	}
	if appState.Token != "" || appState.Student != nil || appState.TaskOptions != nil || appState.TaskRunning {
		t.Fatalf("状态未清空: %+v", appState)
	}
	if appState.Username == "" || appState.Password == "" {
		t.Fatal("凭据不应被清除 (中断续跑依赖)")
	}
	b, err := os.ReadFile(*stateFile)
	if err != nil {
		t.Fatalf("状态文件未落盘: %v", err)
	}
	if strings.Contains(string(b), "\"token\": \"tok\"") {
		t.Fatal("落盘状态仍含旧 token")
	}
}
