// anyview-idle: 自动待机刷 accumTime 的小服务。
// 后端 Go, 前端纯 HTML( web/ 内嵌进二进制)。多账号: 每个浏览器一条 cookie (aid_sid)
// 对应一个独立会话, 各自登录/刷课/看状态互不可见; 账号并行刷, 上游请求共享 -rpm 全局限速。
// 流程: 登录 → 拿课程配置 → 选课程和每题目标区间(分钟, 随机) → 开刷;
// 刷前发现某题已达标(accum ≥ 下限)就跳过。
package main

import (
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"anyview-idle/internal/anyview"
)

//go:embed web
var webFS embed.FS

type AppState struct {
	Username    string                `json:"username,omitempty"`
	Password    string                `json:"password,omitempty"`
	Token       string                `json:"token,omitempty"`
	XFF         string                `json:"xff,omitempty"`
	Student     *anyview.Student      `json:"student,omitempty"`
	TaskRunning bool                  `json:"taskRunning"`
	TaskOptions *anyview.BrushOptions `json:"taskOptions,omitempty"`
	// ResumeAt 非空 = 有一次"到点重登并续跑"的计划 (token 失效让位期间)
	ResumeAt *time.Time `json:"resumeAt,omitempty"`
}

var (
	stateFile    = flag.String("state", ".anyview-idle-state.json", "持久化状态文件路径")
	reloginWait  = flag.Duration("relogin-wait", 3*time.Hour, "token 失效后让位多久再自动重登续跑 (用户优先)")
	reloginRetry = 30 * time.Minute // 重登撞上网络/服务端问题时的重试间隔
	rpm          = flag.Int("rpm", 20, "全局上游限速 (每分钟请求数, 所有账号共享); 0 关闭")
)

// isLoginRejected 服务端明确拒绝登录 (密码错/锁号等): 不重试, 防连续错锁号。
func isLoginRejected(err error) bool {
	return strings.Contains(err.Error(), "登录失败: ")
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
	s, err := ensureSession(w, r)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	s.mgr.SetSession(client, student)

	// 后端持久化记录用户名、密码及生成的 Token。
	// SetSession 会停掉在跑的任务, 必须把 TaskRunning 一并清掉, 否则崩溃后会用旧 options 自动恢复。
	// 手动登录 = 用户接管, 取消待执行的自动重登续跑。
	store.mu.Lock()
	if req.Token != "" {
		s.state.Token = req.Token
	} else {
		s.state.Username = req.Username
		s.state.Password = req.Password
		s.state.Token = client.Token
	}
	s.state.XFF = req.XFF
	s.state.Student = student
	s.state.TaskRunning = false
	s.state.ResumeAt = nil
	stopResumeTimer(s)
	store.saveLocked()
	store.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"student": student,
		"courses": courses,
		"token":   client.Token,
	})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	if s := sessionFromCookie(r); s != nil {
		s.mgr.Stop()
		s.mgr.SetSession(nil, nil)
		store.remove(s.id)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]string{"ok": "logged_out"})
}

func handleCourses(w http.ResponseWriter, r *http.Request) {
	s := sessionFromCookie(r)
	if s == nil {
		writeErr(w, http.StatusUnauthorized, errors.New("未登录"))
		return
	}
	client := s.mgr.Client()
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
	s := sessionFromCookie(r)
	if s == nil {
		writeErr(w, http.StatusUnauthorized, errors.New("未登录"))
		return
	}
	var opts anyview.BrushOptions
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.mgr.Start(opts); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}

	store.mu.Lock()
	s.state.TaskRunning = true
	s.state.TaskOptions = &opts
	s.state.ResumeAt = nil // 手动开任务 = 用户接管, 取消自动重登计划
	stopResumeTimer(s)
	store.saveLocked()
	store.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{"ok": "started"})
}

func handleStop(w http.ResponseWriter, r *http.Request) {
	s := sessionFromCookie(r)
	if s == nil {
		writeErr(w, http.StatusUnauthorized, errors.New("未登录"))
		return
	}
	s.mgr.Stop()

	store.mu.Lock()
	s.state.TaskRunning = false
	store.saveLocked()
	store.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{"ok": "stopping"})
}

func handleCatalog(w http.ResponseWriter, r *http.Request) {
	s := sessionFromCookie(r)
	if s == nil {
		writeErr(w, http.StatusUnauthorized, errors.New("未登录"))
		return
	}
	client := s.mgr.Client()
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

func handleStatus(w http.ResponseWriter, r *http.Request) {
	s := sessionFromCookie(r)
	// 无会话 (新访客/cookie 已失效) 时各字段保持零值, 响应结构与已登录完全一致
	st := anyview.RunStatus{Log: []string{}}
	var client *anyview.Client
	var savedOpts *anyview.BrushOptions
	var savedToken string
	var savedResume *time.Time
	if s != nil {
		st = s.mgr.Status()
		client = s.mgr.Client()
		store.mu.Lock()
		savedOpts = s.state.TaskOptions
		savedToken = s.state.Token
		savedResume = s.state.ResumeAt
		store.mu.Unlock()
	}

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
		"resumeAt":      savedResume,
	}
	writeJSON(w, http.StatusOK, resp)
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8765", "监听地址 (绑本机, 公开访问走反代)")
	flag.Parse()

	anyview.SetUpstreamRPM(*rpm)
	store = newSessionStore(*stateFile)
	store.load()
	for _, s := range store.all() {
		s.restore()
	}

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

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Printf("anyview-idle 已启动: http://%s", *addr)
	log.Fatal(srv.ListenAndServe())
}
