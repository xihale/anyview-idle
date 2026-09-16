// Package anyview 实现 AnyView 平台的登录、课程/目录读取。
// 协议出处: fuckView 仓库 analysis/02(登录)、09(WAF/XFF)、10(计时)。
package anyview

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	BaseURL   = "https://anyview.gdut.edu.cn"
	WSBaseURL = "wss://anyview.gdut.edu.cn/api/exam-service/websocket"
	DefaultUA = "Teru-Hack/FuckVersion.0"
	maxBody   = 8 << 20
)

// 服务端成功码: 200 现行; 31/41/70 旧成功码 (fuckView common.ts 全集, 实测
// scheme/list/student 回 31、scheme/list/student/{eid} 回 41)。
func codeOK(code float64) bool {
	return code == 200 || code == 31 || code == 41 || code == 70
}

type Client struct {
	HTTP  *http.Client
	Jar   *cookiejar.Jar // 保住 WAF 下发的 wzws_sessionid
	Token string
	XFF   string // 网关把会话绑在 XFF 客户端 IP 上, 直连源站时必填且前后一致
	UA    string
}

func NewClient(xff, ua string) *Client {
	if ua == "" {
		ua = DefaultUA
	}
	jar, _ := cookiejar.New(nil)
	return &Client{
		HTTP: &http.Client{Jar: jar, Timeout: 30 * time.Second},
		Jar:  jar,
		XFF:  xff,
		UA:   ua,
	}
}

func (c *Client) setHeaders(h http.Header) {
	h.Set("Accept", "application/json, text/plain, */*")
	h.Set("User-Agent", c.UA)
	if c.Token != "" {
		h.Set("Token", c.Token)
	}
	if c.XFF != "" {
		h.Set("X-Forwarded-For", c.XFF)
	}
}

type envelope struct {
	Code    float64         `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (c *Client) do(ctx context.Context, method, path string, contentType string, body io.Reader) (*envelope, error) {
	if sharedLimiter != nil {
		if err := sharedLimiter.wait(ctx); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req.Header)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxBody {
		return nil, fmt.Errorf("响应过大 (>%d bytes)", maxBody)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("响应非 JSON: %s", truncate(string(raw), 200))
	}
	return &env, nil
}

func (c *Client) get(ctx context.Context, path string) (*envelope, error) {
	return c.do(ctx, http.MethodGet, path, "", nil)
}

// postForm multipart/form-data POST (login/compile/runGroup 同款表单封装)
func (c *Client) postForm(ctx context.Context, path string, fields [][2]string) (*envelope, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, f := range fields {
		fw, err := w.CreateFormField(f[0])
		if err != nil {
			return nil, err
		}
		fw.Write([]byte(f[1]))
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return c.do(ctx, http.MethodPost, path, w.FormDataContentType(), &buf)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…"
}

// ---------- 登录 ----------

type Student struct {
	ID   int64  `json:"id"`
	No   string `json:"no"`
	Name string `json:"name"`
}

// Login 走前端同款流程: GET rsa 拿公钥 → RSA-PKCS1 加密 "密码,毫秒时间戳"(防重放)
// → multipart POST login。密码错误只回 code=1, 无锁定提示, 但连续失败有锁号风险。
func (c *Client) Login(ctx context.Context, username, password string) (*Student, error) {
	env, err := c.get(ctx, "/api/user-service/rsa")
	if err != nil {
		return nil, fmt.Errorf("取 RSA 公钥失败: %w", err)
	}
	if !codeOK(env.Code) {
		return nil, fmt.Errorf("取 RSA 公钥失败: code=%v %s", env.Code, env.Message)
	}
	var pubB64 string
	if err := json.Unmarshal(env.Data, &pubB64); err != nil {
		return nil, fmt.Errorf("RSA 公钥格式异常: %w", err)
	}
	der, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil {
		return nil, fmt.Errorf("RSA 公钥 base64 解码失败: %w", err)
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("RSA 公钥解析失败: %w", err)
	}
	pub, isRSA := parsed.(*rsa.PublicKey)
	if !isRSA {
		return nil, errors.New("公钥不是 RSA 类型")
	}
	plain := fmt.Sprintf("%s,%d", password, time.Now().UnixMilli())
	enc, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(plain))
	if err != nil {
		return nil, err
	}

	env, err = c.postForm(ctx, "/api/user-service/login", [][2]string{
		{"username", username},
		{"password", base64.StdEncoding.EncodeToString(enc)},
		{"roleId", "3"},    // 学生 = 3
		{"schoolId", "63"}, // 广东工业大学
	})
	if err != nil {
		return nil, fmt.Errorf("登录请求失败: %w", err)
	}
	if !codeOK(env.Code) {
		return nil, fmt.Errorf("登录失败: %s", env.Message)
	}
	var data struct {
		Student *Student `json:"student"`
		Token   string   `json:"token"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return nil, fmt.Errorf("登录响应解析失败: %w", err)
	}
	if data.Token == "" {
		return nil, errors.New("登录响应缺少 token")
	}
	c.Token = data.Token
	return data.Student, nil
}

// UseToken 直接复用既有 JWT (约 12h 有效), 跳过密码登录。
func (c *Client) UseToken(token string) { c.Token = token }

// ---------- 课程列表 ----------

type Course struct {
	CourseID   int64  `json:"courseId,omitempty"`
	CourseName string `json:"courseName"`
	SchemeID   int64  `json:"schemeId"`
	ClassID    int64  `json:"classId"`
	TermName   string `json:"termName,omitempty"`
}

// Courses 学生端「我的课程」。优先走 scheme-service/scheme/list/student
// (2026 后端实测: 行内自带 id=schemeId + classId, 且回旧成功码 31);
// 旧端点 exercise-service/course/stu/list 只回 course 表(无 scheme/class), 留作兜底。
func (c *Client) Courses(ctx context.Context) ([]Course, error) {
	courses, err := c.coursesFromScheme(ctx)
	if err != nil || len(courses) == 0 {
		courses, err = c.coursesFromExercise(ctx)
	}
	if err != nil {
		return nil, err
	}
	// 实验课程黑名单: 不允许刷实验课
	var filtered []Course
	for _, it := range courses {
		if strings.Contains(it.CourseName, "实验") {
			continue
		}
		filtered = append(filtered, it)
	}
	return filtered, nil
}

func (c *Client) coursesFromScheme(ctx context.Context) ([]Course, error) {
	env, err := c.get(ctx, "/api/scheme-service/scheme/list/student")
	if err != nil {
		return nil, fmt.Errorf("获取课程列表失败: %w", err)
	}
	if !codeOK(env.Code) {
		return nil, fmt.Errorf("获取课程列表失败: code=%v %s", env.Code, env.Message)
	}
	var data any
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return nil, err
	}
	var courses []Course
	for _, row := range findRows(data, 0) {
		m := asMap(row)
		if m == nil {
			continue
		}
		if course, ok := rowToCourse(m); ok {
			courses = append(courses, course)
		}
	}
	return courses, nil
}

// coursesFromExercise 旧端点兜底: 只回 course 表行, 归一化后凑不齐 scheme/class 的行会被丢弃。
func (c *Client) coursesFromExercise(ctx context.Context) ([]Course, error) {
	env, err := c.get(ctx, "/api/exercise-service/course/stu/list")
	if err != nil {
		return nil, fmt.Errorf("获取课程列表失败: %w", err)
	}
	if !codeOK(env.Code) {
		return nil, fmt.Errorf("获取课程列表失败: code=%v %s", env.Code, env.Message)
	}
	var data any
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return nil, err
	}
	var courses []Course
	for _, row := range findRows(data, 0) {
		m := asMap(row)
		if m == nil {
			continue
		}
		if course, ok := rowToCourse(m); ok {
			courses = append(courses, course)
		}
	}
	return courses, nil
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func numOf(m map[string]any, names ...string) (float64, bool) {
	if m == nil {
		return 0, false
	}
	for _, name := range names {
		switch v := m[name].(type) {
		case float64:
			return v, true
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				return f, true
			}
		}
	}
	return 0, false
}

func strOf(m map[string]any, names ...string) (string, bool) {
	if m == nil {
		return "", false
	}
	for _, name := range names {
		if s, isStr := m[name].(string); isStr && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s), true
		}
	}
	return "", false
}

func findRows(v any, depth int) []any {
	if depth > 4 {
		return nil
	}
	if s := asSlice(v); s != nil {
		return s
	}
	m := asMap(v)
	if m == nil {
		return nil
	}
	for _, key := range []string{"rows", "records", "list", "courses", "courseList", "schemes", "schemeList", "content", "items"} {
		if s := asSlice(m[key]); s != nil {
			return s
		}
	}
	return findRows(m["data"], depth+1)
}

func rowToCourse(row map[string]any) (Course, bool) {
	merged := map[string]any{}
	for k, v := range asMap(row["course"]) {
		merged[k] = v
	}
	for k, v := range row {
		merged[k] = v
	}
	scheme := asMap(row["scheme"])
	class := asMap(row["class"])

	schemeID, ok := numOf(merged, "schemeId", "schemeID", "scheme_id")
	if !ok {
		schemeID, ok = numOf(scheme, "id", "schemeId")
	}
	if !ok {
		schemeID, ok = numOf(merged, "id") // 某些版本把作业表 ID 放行 id 字段
	}
	classID, ok2 := numOf(merged, "classId", "classID", "class_id", "clazzId", "stuClassId")
	if !ok2 {
		classID, ok2 = numOf(class, "id", "classId")
	}
	if !ok || !ok2 {
		return Course{}, false
	}
	// scheme 行的 name 是「课程(班级)-学期」全名, 比courseName更可区分, 优先
	name, _ := strOf(merged, "name", "courseName", "course_name", "cname", "title")
	if name == "" {
		name = fmt.Sprintf("课程 %d", int64(schemeID))
	}
	term, _ := strOf(merged, "termName", "term", "semester")
	course := Course{
		CourseName: name,
		SchemeID:   int64(schemeID),
		ClassID:    int64(classID),
		TermName:   term,
	}
	if id, has := numOf(merged, "courseId", "courseID", "course_id", "id"); has {
		course.CourseID = int64(id)
	}
	return course, true
}

// ---------- 目录 (含 accumTime) ----------

type CatalogItem struct {
	EID          int64  `json:"eid"`
	PName        string `json:"pname"`
	Pass         bool   `json:"pass"`
	AccumTime    int64  `json:"accumTime"` // 秒
	QuestionType int    `json:"questionType"`
	ChapName     string `json:"chapName"`
}

// Catalog GET scheme-service/catalog/list, accumTime 单位秒, 唯一来源是 exam WS 401 计时对。
func (c *Client) Catalog(ctx context.Context, schemeID, classID int64) ([]CatalogItem, error) {
	q := url.Values{}
	q.Set("schemeId", fmt.Sprintf("%d", schemeID))
	q.Set("classId", fmt.Sprintf("%d", classID))
	env, err := c.get(ctx, "/api/scheme-service/catalog/list?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("获取目录失败: %w", err)
	}
	if !codeOK(env.Code) {
		return nil, fmt.Errorf("获取目录失败: code=%v %s", env.Code, env.Message)
	}
	var items []CatalogItem
	if err := json.Unmarshal(env.Data, &items); err != nil {
		var raw any
		if err2 := json.Unmarshal(env.Data, &raw); err2 == nil {
			rows := findRows(raw, 0)
			if len(rows) > 0 {
				if b, err3 := json.Marshal(rows); err3 == nil {
					var fallbackItems []CatalogItem
					if err4 := json.Unmarshal(b, &fallbackItems); err4 == nil {
						return fallbackItems, nil
					}
				}
			}
		}
		return nil, fmt.Errorf("目录解析失败: %w", err)
	}
	return items, nil
}

// ---------- gdb-service: 编译 / 成组判题 / 代码存档 ----------

type CompileData struct {
	/** 含 "编译成功" 即编译通过 */
	Result        string `json:"result"`
	CmpRightCount int64  `json:"cmpRightCount"`
	CmpErrorCount int64  `json:"cmpErrorCount"`
}

type RunData struct {
	Exception     string `json:"exception"`
	RunErrCount   int64  `json:"runErrCount"`
	RunRightCount int64  `json:"runRightCount"`
	NeedInput     bool   `json:"needInput"`
	Output        string `json:"output"`
	End           bool   `json:"end"`
	Passed        bool   `json:"passed"`
	Order         int64  `json:"order"`
}

func (c *Client) Compile(ctx context.Context, eID int64, codeB64, qfn string) (*CompileData, error) {
	env, err := c.postForm(ctx, "/api/gdb-service/code/compile", [][2]string{
		{"questionRes.questionFullName", qfn},
		{"eID", fmt.Sprintf("%d", eID)},
		{"stuCode", codeB64},
		{"isTeacher", "false"},
		{"isDebug", "false"},
	})
	if err != nil {
		return nil, fmt.Errorf("编译请求失败: %w", err)
	}
	if !codeOK(env.Code) {
		return nil, fmt.Errorf("编译失败: code=%v %s", env.Code, env.Message)
	}
	var data CompileData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return nil, fmt.Errorf("编译响应解析失败: %w", err)
	}
	return &data, nil
}

func CompileOK(d *CompileData) bool { return strings.Contains(d.Result, "编译成功") }

// RunGroup kind/language 是判题组解析的关键: 用错会"判题空转"(right=0 err=0 end=true,
// analysis/09 借题实验同款症状), 必须用 scheme/list/student/{eid} 回的题目真实值。
func (c *Client) RunGroup(ctx context.Context, eID int64, codeB64, qfn string, kind, language int64) (*RunData, error) {
	env, err := c.postForm(ctx, "/api/gdb-service/code/runGroup", [][2]string{
		{"questionFullName", qfn},
		{"eID", fmt.Sprintf("%d", eID)},
		{"isTeacher", "false"},
		{"kind", fmt.Sprintf("%d", kind)},
		{"language", fmt.Sprintf("%d", language)},
		{"stuCode", codeB64},
	})
	if err != nil {
		return nil, fmt.Errorf("判题请求失败: %w", err)
	}
	if !codeOK(env.Code) {
		return nil, fmt.Errorf("判题失败: code=%v %s", env.Code, env.Message)
	}
	var data RunData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return nil, fmt.Errorf("判题响应解析失败: %w", err)
	}
	return &data, nil
}

// QuestionMeta 是 scheme/list/student/{eid} 的题目档案: 代码存档 + 判题参数。
type QuestionMeta struct {
	StuCode          string `json:"stuCode"`
	Kind             int64  `json:"kind"`
	Language         int64  `json:"language"`
	QuestionFullName string `json:"questionFullName"`
}

// StuCodeSnapshot 读题目档案 (实测 code=41 旧成功码)。stuCode 为 base64,
// 这里原样透传 (快照→回写逐字节还原, restore-stucode.ts 同款做法)。
func (c *Client) StuCodeSnapshot(ctx context.Context, eID int64) (*QuestionMeta, error) {
	env, err := c.get(ctx, fmt.Sprintf("/api/scheme-service/scheme/list/student/%d", eID))
	if err != nil {
		return nil, err
	}
	if !codeOK(env.Code) {
		return nil, fmt.Errorf("code=%v %s", env.Code, env.Message)
	}
	var meta QuestionMeta
	if err := json.Unmarshal(env.Data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// SaveStuCode 回写代码存档 (前端"保存代码"同款接口)。注意: compile/runGroup
// 会顺带把提交的代码覆盖到存档, 任何注入动作前后必须快照/回写 (analysis/09)。
func (c *Client) SaveStuCode(ctx context.Context, eID int64, codeB64 string) error {
	body, err := json.Marshal(map[string]string{
		"eID":         fmt.Sprintf("%d", eID),
		"studentCode": codeB64,
	})
	if err != nil {
		return err
	}
	env, err := c.do(ctx, http.MethodPost, "/api/gdb-service/exercise/saveStudentCode", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	if !codeOK(env.Code) {
		return fmt.Errorf("code=%v %s", env.Code, env.Message)
	}
	return nil
}

// ExerciseRecord 记录单道题目的服务端历史统计 (编译/运行错误计数, accumTime 等)
type ExerciseRecord struct {
	EID           int64 `json:"eid"`
	AccumTime     int64 `json:"accumTime"`
	RunErrCount   int64 `json:"runErrCount"`
	CmpErrorCount int64 `json:"cmpErrorCount"`
	CmpRightCount int64 `json:"cmpRightCount"`
	RunRightCount int64 `json:"runRightCount"`
}

type ExerciseDetailResponse struct {
	Exercise *ExerciseRecord `json:"exercise"`
}

// ExerciseDetail 获取题目的服务端详细统计 (含已有编译与运行错误计数)。
func (c *Client) ExerciseDetail(ctx context.Context, eID int64) (*ExerciseRecord, error) {
	env, err := c.get(ctx, fmt.Sprintf("/api/scheme-service/scheme/list/student/detail/%d", eID))
	if err != nil {
		return nil, err
	}
	if !codeOK(env.Code) {
		return nil, fmt.Errorf("code=%v %s", env.Code, env.Message)
	}
	var resp ExerciseDetailResponse
	if err := json.Unmarshal(env.Data, &resp); err != nil {
		return nil, err
	}
	if resp.Exercise == nil {
		return &ExerciseRecord{EID: eID}, nil
	}
	return resp.Exercise, nil
}
