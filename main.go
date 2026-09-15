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
	"strings"
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
	// ResumeAt 非空 = 有一次"到点重登并续跑"的计划 (token 失效让位期间)
	ResumeAt *time.Time `json:"resumeAt,omitempty"`
}

var (
	stateFile    = flag.String("state", ".anyview-idle-state.json", "持久化状态文件路径")
	reloginWait  = flag.Duration("relogin-wait", 3*time.Hour, "token 失效后让位多久再自动重登续跑 (用户优先)")
	reloginRetry = 30 * time.Minute // 重登撞上网络/服务端问题时的重试间隔
	stateMu      sync.Mutex
	appState     AppState
	resumeTimer  *time.Timer
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
	tmp := *stateFile + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, *stateFile); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func saveState() error {
	stateMu.Lock()
	defer stateMu.Unlock()
	return saveStateLocked()
}

func stopResumeTimerLocked() {
	if resumeTimer != nil {
		resumeTimer.Stop()
		resumeTimer = nil
	}
}

// planResumeLocked 安排一次延迟重登续跑 (不落盘, 由调用方 saveStateLocked); 调用方持有 stateMu。
func planResumeLocked(d time.Duration) {
	t := time.Now().Add(d)
	appState.ResumeAt = &t
	if resumeTimer != nil {
		resumeTimer.Stop()
	}
	resumeTimer = time.AfterFunc(d, tryResume)
}

// isLoginRejected 服务端明确拒绝登录 (密码错/锁号等): 不重试, 防连续错锁号。
func isLoginRejected(err error) bool {
	return strings.Contains(err.Error(), "登录失败: ")
}

// tryResume 到点重登并续跑。用户期间已手动登录/开任务则放弃本次安排;
// 密码被拒则终止计划 (防锁号); 网络/服务端问题 reloginRetry 后再试。
func tryResume() {
	stateMu.Lock()
	if appState.ResumeAt == nil || appState.TaskOptions == nil || appState.TaskRunning {
		stateMu.Unlock()
		return
	}
	if appState.Username == "" || appState.Password == "" {
		appState.ResumeAt = nil
		stopResumeTimerLocked()
		_ = saveStateLocked()
		stateMu.Unlock()
		log.Printf("无保存的账号密码, 无法自动重登, 请手动登录后重新开刷")
		return
	}
	opts := *appState.TaskOptions
	user, pass, xff := appState.Username, appState.Password, appState.XFF
	stateMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := anyview.NewClient(xff, "")
	student, err := client.Login(ctx, user, pass)
	if err == nil {
		_, err = client.Courses(ctx)
	}
	if err != nil {
		stateMu.Lock()
		if isLoginRejected(err) {
			appState.ResumeAt = nil
			stopResumeTimerLocked()
			_ = saveStateLocked()
			stateMu.Unlock()
			log.Printf("自动重登被拒 (%v), 已取消续跑计划, 请手动登录核实密码", err)
			return
		}
		planResumeLocked(reloginRetry)
		_ = saveStateLocked()
		stateMu.Unlock()
		log.Printf("自动重登失败 (%v), %v 后再试", err, reloginRetry)
		return
	}

	mgr.SetSession(client, student)
	stateMu.Lock()
	// 等待期间用户可能已手动登录接管 (ResumeAt 被清)
	if appState.ResumeAt == nil {
		stateMu.Unlock()
		return
	}
	appState.ResumeAt = nil
	appState.Token = client.Token
	appState.Student = student
	appState.TaskRunning = true
	_ = saveStateLocked()
	stateMu.Unlock()
	if err := mgr.Start(opts); err != nil {
		stateMu.Lock()
		appState.TaskRunning = false
		_ = saveStateLocked()
		stateMu.Unlock()
		log.Printf("续跑启动失败: %v", err)
		return
	}
	log.Printf("已自动重登 (%s) 并续跑: scheme=%d class=%d [%.1f, %.1f]m",
		student.Name, opts.SchemeID, opts.ClassID, opts.LoMin, opts.HiMin)
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

	// 后端持久化记录用户名、密码及生成的 Token。
	// SetSession 会停掉在跑的任务, 必须把 TaskRunning 一并清掉, 否则崩溃后会用旧 options 自动恢复。
	// 手动登录 = 用户接管, 取消待执行的自动重登续跑。
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
	appState.TaskRunning = false
	appState.ResumeAt = nil
	stopResumeTimerLocked()
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
	appState.ResumeAt = nil // 手动开任务 = 用户接管, 取消自动重登计划
	stopResumeTimerLocked()
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
	savedResume := appState.ResumeAt
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
		"resumeAt":      savedResume,
	}
	writeJSON(w, http.StatusOK, resp)
}

// finishLogout 任务结束后自动下线: 清除会话与持久化 token, 不再占号。
// 任务出错中止 (token 失效/被踢/网络等) 时保留任务并安排延迟重登续跑——
// 用户优先: 先下线让位, 不立刻抢回会话。收尾瞬间用户已开出新一轮时跳过。
func finishLogout(gainedSec int64, errMsg string) {
	if mgr.Status().Running {
		return
	}
	mgr.SetSession(nil, nil)
	stateMu.Lock()
	resume := errMsg != "" && appState.TaskOptions != nil
	appState.TaskRunning = false
	appState.Token = ""
	appState.Student = nil
	if resume {
		planResumeLocked(*reloginWait)
	} else {
		appState.TaskOptions = nil
		appState.ResumeAt = nil
		stopResumeTimerLocked()
	}
	_ = saveStateLocked()
	stateMu.Unlock()
	if resume {
		log.Printf("任务出错中止 (%s), 已下线让位; %v 后自动重登续跑", errMsg, reloginWait)
	} else {
		log.Printf("任务结束, 已自动下线并清除本地会话 (本轮挂机 +%.1f 分钟)", float64(gainedSec)/60)
	}
}

func initSessionAndTask() {
	loadState()
	stateMu.Lock()
	st := appState
	stateMu.Unlock()

	if st.Token == "" && (st.Username == "" || st.Password == "") {
		return
	}
	// 只为未完成的任务自动上线; 平时重启保持离线, 不占账号
	if st.TaskOptions == nil || (!st.TaskRunning && st.ResumeAt == nil) {
		log.Printf("无待恢复任务, 保持离线")
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
			mgr.SetSession(client, student)
			log.Printf("已有 Token 仍有效, 直接续跑未完成任务: 方案=%d 班级=%d [%.1f, %.1f]m",
				st.TaskOptions.SchemeID, st.TaskOptions.ClassID, st.TaskOptions.LoMin, st.TaskOptions.HiMin)
			stateMu.Lock()
			appState.ResumeAt = nil
			appState.TaskRunning = true
			_ = saveStateLocked()
			stateMu.Unlock()
			if err := mgr.Start(*st.TaskOptions); err != nil {
				stateMu.Lock()
				appState.TaskRunning = false
				_ = saveStateLocked()
				stateMu.Unlock()
				log.Printf("续跑启动失败: %v", err)
			}
			return
		} else {
			log.Printf("已有 Token 已失效 (%v)", err)
		}
	}

	// 2. token 不可用 → 用户优先: 不立刻重登抢会话, 让位后再自动重登续跑
	stateMu.Lock()
	defer stateMu.Unlock()
	// 旧进程的"在跑"标记已失效, 待办由 ResumeAt 表达; 不清掉会被 tryResume 的守卫拒掉
	appState.TaskRunning = false
	if st.ResumeAt != nil && st.ResumeAt.After(time.Now()) {
		planResumeLocked(time.Until(*st.ResumeAt))
		log.Printf("维持原有安排: %v 后自动重登续跑", time.Until(*st.ResumeAt).Round(time.Minute))
	} else {
		planResumeLocked(*reloginWait)
		log.Printf("token 失效, 为不抢占你正在用的会话, %v 后自动重登续跑", *reloginWait)
	}
	_ = saveStateLocked()
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8765", "监听地址 (只在本机用, 勿暴露公网)")
	flag.Parse()

	mgr.SetOnFinish(func(st anyview.RunStatus) {
		finishLogout(st.GainedSec, st.Error)
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
