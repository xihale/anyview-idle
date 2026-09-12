// 刷时长编排: 取目录 → 过滤已通过题 → 逐题「抽随机目标点 → 刷前检查(已在区间内则跳过)
// → WS 挂机到目标点 → 复核 accumTime 真涨了 → 错误注入(编译错/运行错, 数量可控)」。
// 区间语义: [lo,hi] 是每题 accumTime 的目标区间(分钟)。accum ≥ lo 即视为已达标跳过
// (含高于 hi 的, 不会往回刷); 否则刷到区间内随机一点。计时器单活, 逐题串行。
package anyview

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	logCapacity = 500
	// 换题间隙, 仿人翻题节奏 (analysis/10)
	gapMin = 8 * time.Second
	gapMax = 23 * time.Second

	maxHoldMin  = 180
	maxInjectHi = 50
)

type BrushOptions struct {
	SchemeID int64   `json:"schemeId"`
	ClassID  int64   `json:"classId"`
	LoMin    float64 `json:"loMin"` // 区间下限, 分钟
	HiMin    float64 `json:"hiMin"` // 区间上限, 分钟
	// 每题注入的编译/运行错误尝试次数区间(随机抽), 0-0 = 不注入。
	// 每次尝试随机为「编译错误」或「编译过但运行错」; 运行正确绝不注入(会判过改 pass)。
	InjectLo int `json:"injectLo"`
	InjectHi int `json:"injectHi"`
	// 本次最多处理几题, 0 = 全部
	Limit int `json:"limit,omitempty"`
	// 题目起始偏移 (0-indexed)
	Offset int `json:"offset,omitempty"`
}

type CurrentInfo struct {
	EID       int64     `json:"eid"`
	PName     string    `json:"pname"`
	ChapName  string    `json:"chapName"`
	AccumSec  int64     `json:"accumSec"`
	TargetMin float64   `json:"targetMin"`
	StartedAt time.Time `json:"startedAt"`
}

type RunStatus struct {
	Running   bool    `json:"running"`
	Error     string  `json:"error,omitempty"`
	Student   string  `json:"student,omitempty"`
	Course    string  `json:"course,omitempty"`
	LoMin     float64 `json:"loMin,omitempty"`
	HiMin     float64 `json:"hiMin,omitempty"`
	Total     int     `json:"total"`
	Done      int     `json:"done"` // 本轮实际挂了且复核到账
	Skipped   int     `json:"skipped"`
	Failed    int     `json:"failed"`
	GainedSec int64   `json:"gainedSec"`
	// 注入阶段实际产生的错误次数 (编译错误 + 运行错误)
	InjectedErrors int          `json:"injectedErrors"`
	Current        *CurrentInfo `json:"current,omitempty"`
	StartedAt      *time.Time   `json:"startedAt,omitempty"`
	FinishedAt     *time.Time   `json:"finishedAt,omitempty"`
	Log            []string     `json:"log"`
}

type run struct {
	opts   BrushOptions
	client *Client
	cancel context.CancelFunc
	ctx    context.Context
	mgr    *Manager

	mu     sync.Mutex
	status RunStatus
	log    []string
	timer  *Timer
}

type Manager struct {
	mu       sync.Mutex
	client   *Client
	student  *Student
	run      *run // 正在跑的
	last     *run // 上一轮, 完成后供状态展示
	onFinish func(RunStatus)
}

func NewManager() *Manager { return &Manager{} }

func (m *Manager) SetOnFinish(fn func(RunStatus)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onFinish = fn
}

func (m *Manager) LoggedIn() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.client != nil && m.client.Token != ""
}

// SetSession 登录成功(或复用 token)后登记会话; 会同时停掉可能还在跑的旧 run。
func (m *Manager) SetSession(client *Client, student *Student) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.run != nil {
		m.run.cancel()
		m.run = nil
	}
	m.client = client
	m.student = student
}

func (m *Manager) Client() *Client {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.client
}

func (m *Manager) StudentName() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.student == nil {
		return ""
	}
	return m.student.Name
}

// ---------- 日志 ----------

func (r *run) logf(format string, args ...any) {
	line := time.Now().Format("15:04:05") + "  " + fmt.Sprintf(format, args...)
	r.mu.Lock()
	r.log = append(r.log, line)
	if len(r.log) > logCapacity {
		r.log = r.log[len(r.log)-logCapacity:]
	}
	r.mu.Unlock()
}

func (r *run) snapshot() RunStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.status
	s.Log = append([]string(nil), r.log...)
	return s
}

// ---------- 启停 ----------

var ErrAlreadyRunning = errors.New("已有刷机任务在跑, 先停止再开始")

func (m *Manager) Start(opts BrushOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client == nil || m.client.Token == "" {
		return errors.New("未登录")
	}
	if m.run != nil {
		return ErrAlreadyRunning
	}
	if err := opts.validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &run{opts: opts, client: m.client, ctx: ctx, cancel: cancel, mgr: m}
	r.timer = NewTimer(m.client, r.logf)
	now := time.Now()
	r.status = RunStatus{
		Running:   true,
		Student:   m.studentNameLocked(),
		LoMin:     opts.LoMin,
		HiMin:     opts.HiMin,
		StartedAt: &now,
		Log:       []string{},
	}
	m.run = r
	m.last = r
	go r.exec()
	return nil
}

func (m *Manager) studentNameLocked() string {
	if m.student == nil {
		return ""
	}
	return m.student.Name
}

func (m *Manager) Stop() bool {
	m.mu.Lock()
	r := m.run
	m.mu.Unlock()
	if r == nil {
		return false
	}
	r.logf("⏹ 收到停止指令, 当前题挂完本轮即收尾…")
	r.cancel()
	return true
}

func (m *Manager) Status() RunStatus {
	m.mu.Lock()
	r := m.run
	if r == nil {
		r = m.last
	}
	m.mu.Unlock()
	if r == nil {
		return RunStatus{Log: []string{}, Student: m.StudentName()}
	}
	return r.snapshot()
}

// ---------- 主循环 ----------

func (r *run) finish(err error) {
	r.mu.Lock()
	now := time.Now()
	r.status.Running = false
	r.status.FinishedAt = &now
	r.status.Current = nil
	if err != nil {
		r.status.Error = err.Error()
	}
	st := r.status
	st.Log = append([]string(nil), r.log...)
	r.mu.Unlock()
	r.cancel()
	r.mgr.mu.Lock()
	fire := r.mgr.run == r
	if fire {
		r.mgr.run = nil
	}
	cb := r.mgr.onFinish
	r.mgr.mu.Unlock()
	// 被 SetSession 顶掉的旧 run 不再回调, 避免把新任务的 TaskRunning 写回 false
	if fire && cb != nil {
		go cb(st)
	}
}

func (r *run) exec() {
	r.logf("▶ 开始: 区间 [%.1f, %.1f] 分钟, scheme=%d class=%d",
		r.opts.LoMin, r.opts.HiMin, r.opts.SchemeID, r.opts.ClassID)
	if r.opts.InjectHi > 0 {
		r.logf("错误注入: 目标区间 [%d, %d] 次 (已有达标则跳过, 未达标则随机补足)", r.opts.InjectLo, r.opts.InjectHi)
	}

	catalog, err := r.client.Catalog(r.ctx, r.opts.SchemeID, r.opts.ClassID)
	if err != nil {
		r.finish(fmt.Errorf("取目录失败: %w", err))
		return
	}
	totalInCatalog := len(catalog)
	catalog = clipCatalog(catalog, r.opts.Offset, r.opts.Limit)
	var targets []CatalogItem
	for _, q := range catalog {
		if !q.Pass {
			targets = append(targets, q)
		}
	}
	r.mu.Lock()
	r.status.Total = len(targets)
	r.mu.Unlock()
	r.logf("目录 %d 题 (截取范围 %d 题), 未通过 %d 题为目标",
		totalInCatalog, len(catalog), len(targets))

	if len(targets) == 0 {
		r.finish(nil)
		return
	}

	estMin := 0.0
	for _, q := range targets {
		lo, hi := r.opts.LoMin, r.opts.HiMin
		need := lo + rand.Float64()*(hi-lo)
		if cur := q.AccumTime; int64(cur) < int64(lo*60) {
			estMin += need - float64(cur)/60
		}
	}
	r.logf("预计墙钟约 %.1f 小时 (token 有效期约 12h, 中途 119 需重新登录再开一轮, 已达标的题会自动跳过)",
		estMin/60)

	// 每题节奏: 刷前取一次 catalog 确认当前 accumTime/已通过(不达标才挂), 挂完再取一次复核涨幅
	for i, q := range targets {
		if r.ctx.Err() != nil {
			r.finish(nil)
			return
		}
		fresh, err := r.client.Catalog(r.ctx, r.opts.SchemeID, r.opts.ClassID)
		if err != nil {
			r.finish(fmt.Errorf("取目录失败: %w", err))
			return
		}
		cur := findItem(fresh, q.EID)
		if cur == nil {
			r.logf("[%d/%d] %s 不在目录里, 跳过", i+1, len(targets), q.PName)
			r.bump(&r.status.Skipped)
			continue
		}
		if cur.Pass {
			r.logf("[%d/%d] %s 已通过, 跳过", i+1, len(targets), cur.PName)
			r.bump(&r.status.Skipped)
			continue
		}
		accumMin := float64(cur.AccumTime) / 60
		if accumMin >= r.opts.LoMin {
			r.logf("[%d/%d] %s 现累计 %.1f 分钟, 已在 [%.1f, %.1f] 区间内/以上, 跳过",
				i+1, len(targets), cur.PName, accumMin, r.opts.LoMin, r.opts.HiMin)
			r.bump(&r.status.Skipped)
			continue
		}
		target := r.opts.LoMin + rand.Float64()*(r.opts.HiMin-r.opts.LoMin)
		need := target - accumMin
		if need <= 0.05 {
			need = 0.05 // 至少意思一下, 免得 0 分钟空转
		}

		r.logf("[%d/%d] %s (%d) 第%s章 现累计 %ds (%.1f 分钟) → 挂 %.1f 分钟到 %.1f 分钟目标点",
			i+1, len(targets), cur.PName, cur.EID, cur.ChapName, cur.AccumTime, accumMin, need, target)
		started := time.Now()
		r.setCurrent(cur, target, started)
		err = r.timer.Hold(r.ctx, cur.EID, need)
		r.clearCurrent()
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || r.ctx.Err() != nil {
				r.logf("⏹ 已中止, %s 计时已停", cur.PName)
				r.finish(nil)
				return
			}
			if errors.Is(err, ErrKicked) {
				r.bump(&r.status.Failed)
				r.finish(err)
				return
			}
			r.logf("  ✗ 挂机失败: %v", err)
			r.bump(&r.status.Failed)
			continue
		}
		if r.ctx.Err() != nil {
			r.logf("⏹ 已中止, %s 本轮挂完即收尾", cur.PName)
			r.finish(nil)
			return
		}

		after, err := r.client.Catalog(r.ctx, r.opts.SchemeID, r.opts.ClassID)
		if err != nil {
			r.logf("  ⚠ 复核失败: %v", err)
		} else {
			afterItem := findItem(after, cur.EID)
			if afterItem == nil {
				r.logf("  ⚠ 复核时题目消失")
			} else {
				gained := afterItem.AccumTime - cur.AccumTime
				r.addGained(gained)
				if gained > 0 {
					r.logf("  ✓ accumTime %d → %ds (本轮 +%ds)", cur.AccumTime, afterItem.AccumTime, gained)
					r.bump(&r.status.Done)
				} else {
					r.logf("  ⚠ accumTime 没涨 (+%d)——实验题(ES)服务端不计时属正常, 下一轮会按区间重判", gained)
				}
			}
		}

		// 错误注入在挂完之后: 401 停止计时 = 前端"打开题目面板", 编译/运行发生在
		// 面板打开时, 计时阶段保持无打点 (compile/run 不影响 accumTime, analysis/10)
		if r.opts.InjectHi > 0 {
			r.injectErrors(cur)
		}

		// 换题间隙, 仿人翻题节奏
		gap := gapMin + time.Duration(rand.Float64()*(float64(gapMax-gapMin)))
		if err := sleepCtx(r.ctx, gap); err != nil {
			r.finish(nil)
			return
		}
	}
	done, skipped, failed, gained, injected := func() (int, int, int, int64, int) {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.status.Done, r.status.Skipped, r.status.Failed, r.status.GainedSec, r.status.InjectedErrors
	}()
	r.logf("■ 全部目标处理完毕: 挂机 %d, 跳过 %d, 失败 %d, 累计 +%.1f 分钟, 注入错误 %d 次",
		done, skipped, failed, float64(gained)/60, injected)
	r.finish(nil)
}

func findItem(items []CatalogItem, eid int64) *CatalogItem {
	for i := range items {
		if items[i].EID == eid {
			return &items[i]
		}
	}
	return nil
}

func (r *run) bump(field *int) {
	r.mu.Lock()
	*field++
	r.mu.Unlock()
}

func (r *run) addGained(sec int64) {
	r.mu.Lock()
	r.status.GainedSec += sec
	r.mu.Unlock()
}

func (r *run) setCurrent(cur *CatalogItem, target float64, started time.Time) {
	r.mu.Lock()
	r.status.Current = &CurrentInfo{
		EID:       cur.EID,
		PName:     cur.PName,
		ChapName:  cur.ChapName,
		AccumSec:  cur.AccumTime,
		TargetMin: target,
		StartedAt: started,
	}
	r.mu.Unlock()
}

func (r *run) clearCurrent() {
	r.mu.Lock()
	r.status.Current = nil
	r.mu.Unlock()
}

// ---------- 错误注入 (编译错误 / 运行错误, 数量可控) ----------

// 注入用代码安全边界:
//   - 永不注入"运行正确"——runGroup 全对会判过改 pass, 这不是本工具的事;
//   - compile/runGroup 会顺带把提交代码覆盖到学生存档 (analysis/09 实测),
//     所以注入前后必须快照/回写, 回写失败重试 3 次并大声告警;
//   - 实验题(ES, questionType>=3)走另一套 Lab 编译接口, 普通接口打不了, 跳过注入。
//
// calcInjectCount 计算错误注入计划:
// - 如果已有错误数已经在 [lo, hi] 区间内或已超过上限, 则跳过;
// - 否则随机抽取目标 target ∈ [lo, hi], 计算需要补足的错误数 need = target - currentErrors。
func calcInjectCount(currentErrors, lo, hi int) (target, need int, skip bool, reason string) {
	if hi <= 0 || lo > hi {
		return 0, 0, true, "未开启错误注入"
	}
	if lo > 0 && currentErrors >= lo {
		if currentErrors <= hi {
			return 0, 0, true, fmt.Sprintf("已在 [%d, %d] 区间内", lo, hi)
		}
		return 0, 0, true, fmt.Sprintf("已超过上限 %d 次", hi)
	}
	if lo == 0 && currentErrors > 0 {
		return 0, 0, true, fmt.Sprintf("已在 [%d, %d] 区间内", lo, hi)
	}

	target = drawCount(lo, hi)
	if currentErrors >= target {
		return target, 0, true, fmt.Sprintf("已达随机目标 %d 次", target)
	}
	need = target - currentErrors
	return target, need, false, ""
}

func (r *run) injectErrors(q *CatalogItem) {
	if q.QuestionType >= 3 {
		r.logf("  – 实验题走 Lab 编译接口, 不注入错误, 只挂时长")
		return
	}

	var currentErrors int
	var cmpErr, runErr int64
	rec, err := r.client.ExerciseDetail(r.ctx, q.EID)
	if err != nil {
		r.logf("  ⚠ 获取题目历史错误记录失败: %v, 按 0 记录处理", err)
	} else if rec != nil {
		cmpErr = rec.CmpErrorCount
		runErr = rec.RunErrCount
		currentErrors = int(cmpErr + runErr)
	}

	target, need, skip, reason := calcInjectCount(currentErrors, r.opts.InjectLo, r.opts.InjectHi)
	if skip {
		if currentErrors > 0 {
			r.logf("  – 现已有 %d 次错误 (编译 %d/运行 %d), %s, 跳过注入",
				currentErrors, cmpErr, runErr, reason)
		}
		return
	}

	qfn := fmt.Sprintf("第%s章-%s", q.ChapName, q.PName)
	meta, err := r.client.StuCodeSnapshot(r.ctx, q.EID)
	if err != nil {
		r.logf("  ⚠ 取题目档案失败, 跳过错误注入: %v", err)
		return
	}
	if meta.QuestionFullName != "" {
		qfn = meta.QuestionFullName // 用服务端自己的全名, 避免拼法不一致
	}
	own := decodeCode(meta.StuCode) // 学生自己的代码 (未通过的题多半编译得过但跑不对, 最真实)
	broken := corruptCode(own)
	r.logf("  ☞ 现已有 %d 次错误 (编译 %d/运行 %d) → 随机目标 %d 次, 需补注 %d 次 (kind=%d lang=%d)",
		currentErrors, cmpErr, runErr, target, need, meta.Kind, meta.Language)

	injected := 0
	for i := 0; i < need; i++ {
		if r.ctx.Err() != nil {
			break
		}
		if own != "" && rand.IntN(2) == 0 {
			// 编译过但运行错: 直接用学生自己的未通过代码
			cd, err := r.client.Compile(r.ctx, q.EID, encodeCode(own), qfn)
			switch {
			case err != nil:
				r.logf("    [%d/%d] ✗ 编译接口异常: %v", i+1, need, err)
			case !CompileOK(cd):
				r.logf("    [%d/%d] ✗ 编译错误 (cmpErr=%d): %s", i+1, need, cd.CmpErrorCount, firstLine(cd.Result))
				injected++
			default:
				rd, err := r.client.RunGroup(r.ctx, q.EID, encodeCode(own), qfn, meta.Kind, meta.Language)
				switch {
				case err != nil:
					r.logf("    [%d/%d] ✗ 判题接口异常: %v", i+1, need, err)
				case rd.Passed:
					// 学生自己的代码居然全对——如实报告, 题会变已通过
					r.logf("    [%d/%d] ⚠ 运行竟通过 (right=%d)! %s 将变为已通过", i+1, need, rd.RunRightCount, q.PName)
				default:
					r.logf("    [%d/%d] ↯ 运行错误 (通过 %d/失败 %d)", i+1, need, rd.RunRightCount, rd.RunErrCount)
					injected++
				}
			}
		} else {
			// 编译错误: 学生代码里砍一刀 (没有存档就用保底残码)
			cd, err := r.client.Compile(r.ctx, q.EID, encodeCode(broken), qfn)
			switch {
			case err != nil:
				r.logf("    [%d/%d] ✗ 编译接口异常: %v", i+1, need, err)
			case !CompileOK(cd):
				r.logf("    [%d/%d] ✗ 编译错误 (cmpErr=%d): %s", i+1, need, cd.CmpErrorCount, firstLine(cd.Result))
				injected++
			default:
				r.logf("    [%d/%d] – 残码意外编译过了 (cmpRight=%d), 不计入错误", i+1, need, cd.CmpRightCount)
			}
		}
		_ = sleepCtx(r.ctx, 2*time.Second+time.Duration(rand.Float64()*3*float64(time.Second)))
	}
	if injected > 0 {
		r.mu.Lock()
		r.status.InjectedErrors += injected
		r.mu.Unlock()
	}
	r.restoreStuCode(q.EID, meta.StuCode)
}

// restoreStuCode 回写存档并读回核对; 即使注入中途被停止也必须还原, 用独立 context。
func (r *run) restoreStuCode(eID int64, snap string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for attempt := 1; attempt <= 3; attempt++ {
		if err := r.client.SaveStuCode(ctx, eID, snap); err != nil {
			r.logf("  ⚠ 存档回写失败 (第 %d 次): %v", attempt, err)
		} else if now, err := r.client.StuCodeSnapshot(ctx, eID); err != nil {
			r.logf("  ⚠ 存档读回失败 (第 %d 次): %v", attempt, err)
		} else if now.StuCode == snap {
			r.logf("  ✓ 代码存档已还原")
			return
		} else {
			r.logf("  ⚠ 存档读回与快照不一致 (第 %d 次), 重试…", attempt)
		}
		select {
		case <-ctx.Done():
			r.logf("  ⚠️ 存档还原未确认, 请手动核对 %d 的 stuCode!", eID)
			return
		case <-time.After(2 * time.Second):
		}
	}
	r.logf("  ⚠️ 存档还原 3 次未通过, 请手动核对 %d 的 stuCode!", eID)
}

func drawCount(lo, hi int) int {
	if hi <= 0 || lo > hi {
		return 0
	}
	return lo + rand.IntN(hi-lo+1)
}

// decodeCode 存档实测是 base64; 解不出/不像源码就按原文处理 (只用于注入选码, 不回写)。
func decodeCode(snap string) string {
	if snap == "" {
		return ""
	}
	if raw, err := base64.StdEncoding.DecodeString(snap); err == nil && looksLikeCode(raw) {
		return string(raw)
	}
	return snap
}

func encodeCode(code string) string {
	return base64.StdEncoding.EncodeToString([]byte(strings.TrimSpace(code)))
}

func looksLikeCode(raw []byte) bool {
	if !utf8.Valid(raw) || len(raw) == 0 {
		return false
	}
	for _, b := range raw {
		if b < 0x09 || (b > 0x0d && b < 0x20) {
			return false // 控制字符 → 解码出的不是源码
		}
	}
	return true
}

// corruptCode 砍出一份高概率编译不过的残码: 删第一个分号, 没有分号就截掉 30%。
func corruptCode(code string) string {
	if code == "" {
		return "int ;\n" // 保底残码, 独立也编译不过
	}
	if i := strings.Index(code, ";"); i >= 0 {
		return code[:i] + code[i+1:]
	}
	cut := len(code) * 7 / 10
	if cut == 0 {
		return "int ;\n"
	}
	return code[:cut]
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return truncate(s, 120)
}

func (o BrushOptions) validate() error {
	if o.SchemeID <= 0 || o.ClassID <= 0 {
		return errors.New("缺少 schemeId 或 classId")
	}
	if o.LoMin <= 0 || o.HiMin < o.LoMin || o.HiMin > maxHoldMin {
		return errors.New("时长区间不合法 (需 0 < 下限 ≤ 上限 ≤ 180)")
	}
	if o.InjectLo < 0 || o.InjectHi < o.InjectLo || o.InjectHi > maxInjectHi {
		return errors.New("错误次数区间不合法 (需 0 ≤ 下限 ≤ 上限 ≤ 50)")
	}
	if o.Offset < 0 || o.Limit < 0 {
		return errors.New("offset/limit 不能为负")
	}
	return nil
}

func clipCatalog(catalog []CatalogItem, offset, limit int) []CatalogItem {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(catalog) {
		return nil
	}
	catalog = catalog[offset:]
	if limit > 0 && len(catalog) > limit {
		catalog = catalog[:limit]
	}
	return catalog
}
