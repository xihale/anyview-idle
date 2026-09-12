// anyview-idle: 自动待机刷 accumTime 的本地小服务。
// 后端 Go, 前端纯 HTML( web/ 内嵌进二进制)。流程: 登录 → 拿课程配置 →
// 选课程和每题目标区间(分钟, 随机) → 开刷; 刷前发现某题已达标(accum ≥ 下限)就跳过。
package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"anyview-idle/internal/anyview"
)

//go:embed web
var webFS embed.FS

var mgr = anyview.NewManager()

type AppState struct {
	Username    string                `json:"username,omitempty"`
	Password    string                `json:"password,omitempty"`
	Token       string                `json:"token,omitempty"`
	XFF         string                `json:"xff,omitempty"`
	Student     *anyview.Student      `json:"student,omitempty"`
	TaskRunning bool                  `json:"taskRunning"`
	TaskOptions *anyview.BrushOptions `json:"taskOptions,omitempty"`
}

var (
	stateFile = flag.String("state", ".anyview-idle-state.json", "持久化状态文件路径")
	stateMu   sync.Mutex
	appState  AppState
)

func loadState() {
	stateMu.Lock()
	defer stateMu.Unlock()
	b, err := os.ReadFile(*stateFile)
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, &appState)
}

func saveStateLocked() error {
	b, err := json.MarshalIndent(appState, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(*stateFile, b, 0600)
}

func saveState() error {
	stateMu.Lock()
	defer stateMu.Unlock()
	return saveStateLocked()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Token    string `json:"token"` // 填了直接复用, 跳过密码登录
		XFF      string `json:"xff"`   // 直连源站时必填, 走云 WAF 留空
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	client := anyview.NewClient(req.XFF, "")
	var student *anyview.Student
	if req.Token != "" {
		client.UseToken(req.Token)
	} else {
		if req.Username == "" || req.Password == "" {
			writeErr(w, http.StatusBadRequest, errors.New("需要用户名/密码, 或直接填 token"))
			return
		}
		var err error
		student, err = client.Login(r.Context(), req.Username, req.Password)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, err)
			return
		}
	}
	// 登录后先拿一次配置(课程列表), 顺便验证 token 可用
	courses, err := client.Courses(r.Context())
	if err != nil {
		writeErr(w, http.StatusUnauthorized, errors.New("登录后的课程列表拉取失败, token 可能无效: "+err.Error()))
		return
	}
	if student == nil {
		student = &anyview.Student{Name: "token 用户"}
	}
	mgr.SetSession(client, student)

	// 后端持久化记录用户名、密码及生成的 Token
	stateMu.Lock()
	if req.Token != "" {
		appState.Token = req.Token
	} else {
		appState.Username = req.Username
		appState.Password = req.Password
		appState.Token = client.Token
	}
	appState.XFF = req.XFF
	appState.Student = student
	_ = saveStateLocked()
	stateMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"student": student,
		"courses": courses,
		"token":   client.Token,
	})
}

func handleLogout(w http.ResponseWriter, _ *http.Request) {
	mgr.Stop()
	mgr.SetSession(nil, nil)

	stateMu.Lock()
	appState = AppState{}
	_ = saveStateLocked()
	stateMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{"ok": "logged_out"})
}

func handleCourses(w http.ResponseWriter, r *http.Request) {
	client := mgr.Client()
	if client == nil {
		writeErr(w, http.StatusUnauthorized, errors.New("未登录"))
		return
	}
	courses, err := client.Courses(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, courses)
}

func handleStart(w http.ResponseWriter, r *http.Request) {
	var opts anyview.BrushOptions
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := mgr.Start(opts); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}

	stateMu.Lock()
	appState.TaskRunning = true
	appState.TaskOptions = &opts
	_ = saveStateLocked()
	stateMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{"ok": "started"})
}

func handleStop(w http.ResponseWriter, _ *http.Request) {
	mgr.Stop()

	stateMu.Lock()
	appState.TaskRunning = false
	_ = saveStateLocked()
	stateMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{"ok": "stopping"})
}

func handleCatalog(w http.ResponseWriter, r *http.Request) {
	client := mgr.Client()
	if client == nil {
		writeErr(w, http.StatusUnauthorized, errors.New("未登录"))
		return
	}
	schemeID, _ := strconv.ParseInt(r.URL.Query().Get("schemeId"), 10, 64)
	classID, _ := strconv.ParseInt(r.URL.Query().Get("classId"), 10, 64)
	if schemeID == 0 || classID == 0 {
		writeErr(w, http.StatusBadRequest, errors.New("缺少 schemeId 或 classId"))
		return
	}
	catalog, err := client.Catalog(r.Context(), schemeID, classID)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	var unpassed int
	for _, it := range catalog {
		if !it.Pass {
			unpassed++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total":    len(catalog),
		"unpassed": unpassed,
	})
}

func handleStatus(w http.ResponseWriter, _ *http.Request) {
	st := mgr.Status()
	client := mgr.Client()

	stateMu.Lock()
	savedOpts := appState.TaskOptions
	savedToken := appState.Token
	stateMu.Unlock()

	token := ""
	if client != nil && client.Token != "" {
		token = client.Token
	} else if savedToken != "" {
		token = savedToken
	}

	resp := map[string]any{
		"running":        st.Running,
		"error":          st.Error,
		"student":        st.Student,
		"course":         st.Course,
		"loMin":          st.LoMin,
		"hiMin":          st.HiMin,
		"total":          st.Total,
		"done":           st.Done,
		"skipped":        st.Skipped,
		"failed":         st.Failed,
		"injectedErrors": st.InjectedErrors,
		"gainedSec":      st.GainedSec,
		"startedAt":      st.StartedAt,
		"finishedAt":     st.FinishedAt,
		"current":        st.Current,
		"log":            st.Log,
		// 同步字段
		"authenticated": client != nil && client.Token != "",
		"token":         token,
		"savedOptions":  savedOpts,
	}
	writeJSON(w, http.StatusOK, resp)
}

func initSessionAndTask() {
	loadState()
	stateMu.Lock()
	st := appState
	stateMu.Unlock()

	if st.Token == "" && (st.Username == "" || st.Password == "") {
		return
	}

	client := anyview.NewClient(st.XFF, "")
	var student *anyview.Student
	var err error

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1. 优先尝试既有 token
	if st.Token != "" {
		client.UseToken(st.Token)
		_, err = client.Courses(ctx)
		if err == nil {
			student = st.Student
			if student == nil {
				student = &anyview.Student{Name: "token 用户"}
			}
			log.Printf("已有 Token 验证成功，已自动恢复会话: %s", student.Name)
		} else {
			log.Printf("已有 Token 验证失败 (%v)，尝试重新登录", err)
		}
	}

	// 2. 如果 token 失效，且保存了用户名和密码，则自动重新登录生成新 token
	if student == nil && st.Username != "" && st.Password != "" {
		log.Printf("正在使用持久化凭据自动重新登录: %s", st.Username)
		student, err = client.Login(ctx, st.Username, st.Password)
		if err != nil {
			log.Printf("自动重新登录失败: %v", err)
			return
		}
		_, err = client.Courses(ctx)
		if err != nil {
			log.Printf("登录后验证课程失败: %v", err)
			return
		}
		log.Printf("自动重新登录成功: %s, 新 Token 已生成并持久化", student.Name)
		stateMu.Lock()
		appState.Token = client.Token
		appState.Student = student
		_ = saveStateLocked()
		stateMu.Unlock()
	}

	if student != nil {
		mgr.SetSession(client, student)

		// 3. 重启恢复任务和状态：如果上次退出时任务正在运行，则自动恢复任务
		if st.TaskRunning && st.TaskOptions != nil {
			log.Printf("检测到未完成的任务，正在自动恢复运行: 方案=%d 班级=%d [%.1f, %.1f]m",
				st.TaskOptions.SchemeID, st.TaskOptions.ClassID, st.TaskOptions.LoMin, st.TaskOptions.HiMin)
			if err := mgr.Start(*st.TaskOptions); err != nil {
				log.Printf("自动恢复任务失败: %v", err)
				stateMu.Lock()
				appState.TaskRunning = false
				_ = saveStateLocked()
				stateMu.Unlock()
			} else {
				log.Printf("任务已恢复并处于运行态")
			}
		}
	}
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8765", "监听地址 (只在本机用, 勿暴露公网)")
	flag.Parse()

	mgr.SetOnFinish(func(st anyview.RunStatus) {
		stateMu.Lock()
		appState.TaskRunning = false
		_ = saveStateLocked()
		stateMu.Unlock()
	})

	// 启动时初始化恢复会话与任务
	initSessionAndTask()

	static, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServer(http.FS(static)))
	mux.HandleFunc("POST /api/login", handleLogin)
	mux.HandleFunc("POST /api/logout", handleLogout)
	mux.HandleFunc("GET /api/courses", handleCourses)
	mux.HandleFunc("GET /api/catalog", handleCatalog)
	mux.HandleFunc("POST /api/start", handleStart)
	mux.HandleFunc("POST /api/stop", handleStop)
	mux.HandleFunc("GET /api/status", handleStatus)

	log.Printf("anyview-idle 已启动: http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
