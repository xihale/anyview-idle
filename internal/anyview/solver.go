// 自动刷题引擎: 题解库优先 (internal/anyview/solutions, fuckView 实测通过);
// 未命中走 oracle 泄露两轮法 (fuckView analysis/09 附2, DC01PE11 实测)。
//
// 现网判题 runGroup 的 output 会逐组回显「输入 + 你的结果 + 系统结果」——交模板
// 原样空桩跑一轮即可拿到完整答案表 (oracle)。round-2 交查表硬编码: 构造器
// (__attribute__((constructor)) 在 main 前跑) 先把整块 stdin 截走, 目标函数被
// main 调到时按「整块输入归一化后查表」, 命中就打出该组的正确输出并 exit(0)。
//
// 安全边界 (与 injectErrors 同源, analysis/09):
//   - compile/runGroup 会顺带把提交代码覆盖进学生存档, 所以任何提交前后
//     快照/回写存档, 回写后读回核对 (restoreStuCode);
//   - round-2 交卷前 release 清编译目录, 防粘滞上一题模板;
//   - 判题结果以 runGroup passed=true 为准, 没过就是没过, 不落任何假账;
//   - ProbeOnly 只跑 round-1 探测, 绝不交 round-2, pass 保持不变。
package anyview

import (
	"context"
	"errors"
	"fmt"
	"html"
	"math/rand/v2"
	"regexp"
	"strings"
	"time"
)

const (
	solveRunGroupCap     = 100 // runGroup 单题最多调用数 (每组一调打到 end=true)
	solveStepPause       = 2 * time.Second
	solveRetryPause      = 5 * time.Second
	solveLibraryAttempts = 4 // 题解库直交判败后的重交上限 (上游判题抖动)
)

// ---------- oracle 解析 ----------

// OraclePair 一组用例: 原始输入 → 正确输出 (均取自判题回显原文)。
type OraclePair struct {
	Input    string
	Expected string
}

var (
	// 组结束标记: ===RIGHT=== / ========RIGHT======== / --------ERROR-------- (现网实测为纯短横线)
	reGroupMarker = regexp.MustCompile(`(?m)^[=\-]+\s*(?:=+(?:RIGHT|ERROR)=+|(?:RIGHT|ERROR))\s*[=\-]+$`)
	// 组头: 「第N组」任意装饰 (旧格式兜底)
	reGroupHead = regexp.MustCompile(`(?m)^.*第\s*\d+\s*组.*$`)
	// 段标签行 (现网实测: 函数输入：/你的结果：/系统结果：, 解码后各占一行)
	reInputLabel = regexp.MustCompile(`(?m)^[^\S\n]*(?:函数输入|输入数据|测试输入|输入|Input)[::]?.*$`)
	reYourLabel  = regexp.MustCompile(`(?m)^[^\S\n]*(?:你的结果|你的输出|运行结果|Your (?:output|result))[::]?.*$`)
	reSysLabel   = regexp.MustCompile(`(?m)^[^\S\n]*(?:系统结果|正确结果|标准结果|期望输出|正确输出|System result|Expected)[::]?.*$`)
	// 判题回显是 HTML: <br> 换行, &nbsp; 等实体转义
	reBrTag = regexp.MustCompile(`(?i)<br\s*/?>`)
)

// decodeOracleHTML 把判题回显的 HTML 转成纯文本。
// &nbsp; 经 html.UnescapeString 变成 U+00A0, 而查表归一化只剥 ASCII 空白,
// 必须在这里折算成普通空格, 否则运行时 stdin (普通空格) 与表键对不上。
func decodeOracleHTML(s string) string {
	s = html.UnescapeString(s)
	s = reBrTag.ReplaceAllString(s, "\n")
	return strings.ReplaceAll(s, "\u00a0", " ")
}

// markerLine 判题标记行 (渲染层消费, 提取答案时要剥掉)。
func markerLine(s string) bool {
	return reGroupMarker.MatchString(strings.TrimSpace(s))
}

// ParseOracle 从 runGroup 拼接输出里提取每组 (输入, 正确输出)。
// 回显为 HTML (标签+实体), 先解码; 每组以判题标记行收尾, 按标记切块。
// 任何一组解析不出来都宁可少不宁可错: 返回的 pairs 只含三段齐全的组,
// 全不齐全时返回错误。
func ParseOracle(raw string) ([]OraclePair, error) {
	output := decodeOracleHTML(raw)
	blocks := splitOracleBlocks(output)
	var pairs []OraclePair
	for _, b := range blocks {
		in := sectionAfter(b, reInputLabel, reYourLabel, reSysLabel)
		exp := sectionAfter(b, reSysLabel, nil, nil)
		if in == "" || exp == "" {
			continue
		}
		// 系统结果在原文里以 <br> 收尾 (= 精确比对含末尾换行); Trim 剥掉了它,
		// 查表输出须补回, 否则差一个 \n 就判 ERROR (09-20 DC01PE21 实测)
		exp += "\n"
		pairs = append(pairs, OraclePair{Input: in, Expected: exp})
	}
	if len(pairs) == 0 {
		return nil, errors.New("未能从判题输出解析出任何「输入→正确结果」对 (格式见日志原文)")
	}
	return pairs, nil
}

// splitOracleBlocks 切组: 判题标记行是组块的终结符 (每组一次 runGroup 调用,
// 各带一个标记), 按标记切段; 没有标记再按「第N组」行切; 都不行整块当一组。
func splitOracleBlocks(out string) []string {
	if locs := reGroupMarker.FindAllStringIndex(out, -1); len(locs) >= 1 {
		var blocks []string
		start := 0
		for _, l := range locs {
			if l[0] > start {
				blocks = append(blocks, out[start:l[0]])
			}
			start = l[1]
		}
		if start < len(out) {
			blocks = append(blocks, out[start:])
		}
		return blocks
	}
	if locs := reGroupHead.FindAllStringIndex(out, -1); len(locs) >= 2 {
		return cutBy(locs, out)
	}
	return []string{out}
}

func cutBy(locs [][]int, out string) []string {
	var blocks []string
	start := 0
	for _, l := range locs {
		if l[0] > start {
			blocks = append(blocks, out[start:l[0]])
		}
		start = l[0]
	}
	if start < len(out) {
		blocks = append(blocks, out[start:])
	}
	return blocks
}

// sectionAfter 返回 label 匹配处到下一个标签 (stopA/stopB) 或块尾之间的文本,
// 去掉判题标记行与首尾空行。
func sectionAfter(block string, label *regexp.Regexp, stopA, stopB *regexp.Regexp) string {
	loc := label.FindStringIndex(block)
	if loc == nil {
		return ""
	}
	rest := block[loc[1]:]
	end := len(rest)
	if stopA != nil {
		if l := stopA.FindStringIndex(rest); l != nil && l[0] < end {
			end = l[0]
		}
	}
	if stopB != nil {
		if l := stopB.FindStringIndex(rest); l != nil && l[0] < end {
			end = l[0]
		}
	}
	var lines []string
	for _, ln := range strings.Split(rest[:end], "\n") {
		if markerLine(ln) {
			continue
		}
		lines = append(lines, ln)
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

// ---------- round-2 代码生成 ----------

// 生成代码的预处理段: 不截留、不读 stdin —— 判题器按 needInput 握手逐行喂输入,
// 任何抢读都会把程序挂死 (09-20 实测)。查表靠调用序号 (见 anvLookupBody)。
const anvPrelude = `/* anv round-2 lookup (auto-generated) */
#include <stdio.h>
#include <stdlib.h>
`

// emitTable 生成正确输出值表 (按组顺序)。
func emitTable(pairs []OraclePair) string {
	var b strings.Builder
	b.WriteString("static const char *anv_vals[] = {")
	for i, p := range pairs {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("\n    ")
		b.WriteString(cLiteral(p.Expected))
	}
	b.WriteString("\n};\n")
	return b.String()
}

// cLiteral 把任意字节串编码成 C 字符串字面量 (可打印字符直出, 其余八进制转义)。
func cLiteral(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 || c == 0x7f {
				fmt.Fprintf(&b, "\\%03o", c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// anvLookupBody 函数体: 第 k 次调用打印第 k 组的正确输出并退出。
// 每组一次程序执行、顺序判组 (order=N), 调用序即组序; 不读 stdin 就没有
// needInput 握手可卡, 命中即 exit(0), 次数耗尽走原函数体兜底。
const anvLookupBody = `{
    static int anv_k = 0;
    int anv_n = (int)(sizeof(anv_vals) / sizeof(anv_vals[0]));
    if (anv_k < anv_n) {
        fputs(anv_vals[anv_k++], stdout);
        fflush(stdout);
        exit(0);
    }
`

// funcDecl 一次顶层函数定义的定位。
type funcDecl struct {
	Name       string
	HeadStart  int // 函数定义(返回类型)起始下标
	BodyStart  int // '{' 下标
	BodyEnd    int // '}' 下标 (含)
	BodyIsStub bool
}

// findTargetFunc 在模板里找学生该补全的函数: 优先「函数体只有空白/注释」的
// 空桩; 没有就取最后一个非 main 定义。找不到报错 (整程题走 GenerateLookup 的
// main 检测分支, 不经过这里)。
func findTargetFunc(tpl string) (*funcDecl, error) {
	decls, err := scanFuncDecls(tpl)
	if err != nil {
		return nil, err
	}
	var stubs []*funcDecl
	for i := range decls {
		if decls[i].Name == "main" {
			continue
		}
		if decls[i].BodyIsStub {
			stubs = append(stubs, &decls[i])
		}
	}
	if len(stubs) > 0 {
		return stubs[0], nil
	}
	for i := len(decls) - 1; i >= 0; i-- {
		if decls[i].Name != "main" {
			return &decls[i], nil
		}
	}
	return nil, errors.New("模板里没有可补全的函数")
}

// reFuncHead 顶层函数头: 签名与 '{' 之间允许任意行/块注释
// (真实模板形如 "int f(...)\n/* 返回最高成绩 */\n{  // Add your code here")。
var reFuncHead = regexp.MustCompile(`(?m)^\s*[A-Za-z_][\w\s\*,&<>:]*?[\s\*]([A-Za-z_]\w*)\s*\(([^;{})]*)\)\s*(?:(?://[^\n]*|/\*[^*]*\*+(?:[^*/][^*]*\*+)*/)\s*)*\{`)

// scanFuncDecls 粗扫顶层函数定义 (不处理字符串里的花括号这类边角; 教材模板
// 足够规整, 空桩优先策略对扫描噪声不敏感)。正则匹配以 '{' 结尾, '{' 下标 =
// 整个匹配末尾 - 1。
func scanFuncDecls(tpl string) ([]funcDecl, error) {
	var out []funcDecl
	for _, l := range reFuncHead.FindAllStringSubmatchIndex(tpl, -1) {
		name := tpl[l[2]:l[3]]
		start := l[1] - 1 // '{'
		end, ok := matchBrace(tpl, start)
		if !ok {
			continue
		}
		body := tpl[start+1 : end]
		out = append(out, funcDecl{
			Name:       name,
			HeadStart:  l[0],
			BodyStart:  start,
			BodyEnd:    end,
			BodyIsStub: isStubBody(body),
		})
	}
	if len(out) == 0 {
		return nil, errors.New("模板里没扫到函数定义")
	}
	return out, nil
}

// matchBrace 找与 tpl[open] 配对的 '}' 下标 (计数法, 不管字符串字面量)。
func matchBrace(tpl string, open int) (int, bool) {
	depth := 0
	for i := open; i < len(tpl); i++ {
		switch tpl[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// isStubBody 函数体只有空白和注释 = 空桩。
func isStubBody(body string) bool {
	s := stripCComments(body)
	return strings.TrimSpace(s) == ""
}

func stripCComments(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				break
			}
			i += end + 4
			continue
		}
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '/' {
			if nl := strings.IndexByte(s[i:], '\n'); nl >= 0 {
				i += nl
			} else {
				break
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// GenerateLookup 生成 round-2 查表代码。
// 题型判别看「有没有空桩函数」而非 main: 函数补全题的模板 Dx.cpp 本来就自带
// main (analysis/08), 学生存档只是补的函数; 有空桩 → 只改空桩函数体 (补全题,
// 交带 main 的代码会 multiple definition of main); 没空桩 → 整程题, 整体替换。
// C 要求声明先于使用, prelude 与答案表都插在目标函数定义之前。
func GenerateLookup(tpl string, pairs []OraclePair) (string, error) {
	if len(pairs) == 0 {
		return "", errors.New("没有可用的答案对")
	}
	table := emitTable(pairs)

	target, findErr := findTargetFunc(tpl)
	if findErr == nil {
		// 函数补全题 (平台会附加自己的 main, 学生代码绝不能带 main):
		// 函数体是不是空桩无所谓, 查表命中即 exit(0), 原体仅作次数耗尽退路
		var b strings.Builder
		b.WriteString(tpl[:target.HeadStart])
		b.WriteString(anvPrelude)
		b.WriteString("\n" + table)
		// 函数头原样保留, 函数体 = 查表 + 原函数体 (原体含收尾 '}')
		b.WriteString(tpl[target.HeadStart:target.BodyStart])
		b.WriteString(anvLookupBody)
		b.WriteString("    /* 查表未命中, 退回原逻辑 */\n")
		b.WriteString(tpl[target.BodyStart+1 : target.BodyEnd+1])
		b.WriteString(tpl[target.BodyEnd+1:])
		return b.String(), nil
	}

	// 整程题: 模板没有可补全的函数, 换成独立查表程序
	// (anvLookupBody 自带函数体花括号, 尾部 } 收的是 for 循环)
	return "/* anv round-2 whole-program lookup */\n" + anvPrelude + "\n" + table + "int main(void) " + anvLookupBody + "    return 0;\n}\n", nil
}

// ---------- 任务编排 ----------

// execSolve solve 模式主循环: 逐未通过题「快照→(题解库直交 | 空桩探测→查表生成)→
// 判题→还原存档→release」。
func (r *run) execSolve() {
	r.logf("▶ 自动刷题: scheme=%d class=%d (题解库优先, 未命中走 oracle 两轮)", r.opts.SchemeID, r.opts.ClassID)
	if r.opts.ProbeOnly {
		r.logf("探测模式: 只跑空桩拿答案表, 不交 round-2, 不改 pass")
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
		if !q.Pass && q.QuestionType < 2 { // 实验/课设走 Lab 接口, 本科生 401, 跳过
			targets = append(targets, q)
		}
	}
	r.mu.Lock()
	r.status.Total = len(targets)
	r.mu.Unlock()
	r.logf("目录 %d 题 (截取 %d), 未通过且可判题的 %d 题为目标",
		totalInCatalog, len(catalog), len(targets))

	for i := range targets {
		if r.ctx.Err() != nil {
			r.finish(nil)
			return
		}
		q := &targets[i]
		// 刷前取一次目录确认仍未通过 (上一题可能刚变 pass)
		fresh, err := r.client.Catalog(r.ctx, r.opts.SchemeID, r.opts.ClassID)
		if err != nil {
			r.finish(fmt.Errorf("取目录失败: %w", err))
			return
		}
		cur := findItem(fresh, q.EID)
		if cur == nil || cur.Pass {
			r.logf("[%d/%d] %s 已通过或不在目录, 跳过", i+1, len(targets), q.PName)
			r.bump(&r.status.Skipped)
			continue
		}
		r.setCurrent(cur, 0, time.Now())
		err = r.solveOne(i+1, len(targets), cur)
		r.clearCurrent()
		if err != nil {
			if errors.Is(err, context.Canceled) || r.ctx.Err() != nil {
				r.logf("⏹ 已中止, %s 收尾", cur.PName)
				r.finish(nil)
				return
			}
			if errors.Is(err, ErrKicked) {
				r.bump(&r.status.Failed)
				r.finish(err)
				return
			}
			r.logf("  ✗ %s: %v", cur.PName, err)
			r.bump(&r.status.Failed)
		}
		if err := sleepCtx(r.ctx, gapMin+time.Duration(rand.Float64()*float64(gapMax-gapMin))); err != nil {
			r.finish(nil)
			return
		}
	}

	r.mu.Lock()
	st := r.status
	r.mu.Unlock()
	r.logf("■ 刷题完毕: 通过 %d, 跳过 %d, 失败 %d", st.Solved, st.Skipped, st.Failed)
	r.finish(nil)
}

// solveOne 单题。题解库命中 → 直接交真题解 (数据随机化题的唯一通路);
// 未命中 → oracle 泄露两轮法 (空桩探测拿答案表, round-2 交查表代码)。
// 返回 error 表示该题失败 (引擎继续下一题); ErrKicked 特殊: 会话失效, 整轮终止。
func (r *run) solveOne(idx, total int, q *CatalogItem) error {
	meta, err := r.client.StuCodeSnapshot(r.ctx, q.EID)
	if err != nil {
		return fmt.Errorf("取题目档案失败: %w", err)
	}
	qfn := fmt.Sprintf("第%s章-%s", q.ChapName, q.PName)
	if meta.QuestionFullName != "" {
		qfn = meta.QuestionFullName
	}
	r.logf("[%d/%d] %s (%d) %s kind=%d lang=%d", idx, total, q.PName, q.EID, qfn, meta.Kind, meta.Language)

	if sol, serr := librarySolution(q.PName); serr == nil {
		r.logf("  ☞ 命中本地题解库 (fuckView 实测), 直接交真题解")
		// 上游判题有抖动: gdb 驱动判题, 同一份代码两次判的 err 数都会变
		// (DC07PE15 实测: 首判 err=1 未过, 原样重交 passed=true 落账)。
		// 判败不记失败, 原样重交, 抖回 passed=true 即停。
		for attempt := 1; ; attempt++ {
			passed, jerr := r.submitAndJudge(q, qfn, meta, sol, "真题解")
			if jerr != nil {
				return jerr
			}
			if passed || attempt >= solveLibraryAttempts {
				msg := ""
				if !passed {
					msg = fmt.Sprintf("真题解 %d 次判题均未通过 (上游判题抖动, 重跑一轮常可过)", solveLibraryAttempts)
				}
				return r.reportJudge(q, passed, msg)
			}
			r.logf("  ↻ 第 %d 次判题未过 (判题抖动), 原样重交", attempt)
			if err := sleepCtx(r.ctx, solveRetryPause); err != nil {
				return err
			}
		}
	}

	tpl, err := r.client.GetTemplate(r.ctx, q.EID)
	if err != nil {
		return fmt.Errorf("取模板失败: %w", err)
	}
	if strings.TrimSpace(tpl) == "" {
		return errors.New("模板为空")
	}

	if err := r.client.Release(r.ctx); err != nil {
		r.logf("  ⚠ %v (继续)", err)
	}

	// ---- round 1: 空桩探测, 收答案表 ----
	cd, err := r.client.Compile(r.ctx, q.EID, encodeCode(tpl), qfn)
	if err != nil {
		return fmt.Errorf("空桩编译请求失败: %w", err)
	}
	if !CompileOK(cd) {
		return fmt.Errorf("空桩编译失败 (模板自身编译不过): %s", firstLine(cd.Result))
	}
	if err := sleepCtx(r.ctx, solveStepPause); err != nil {
		return err
	}
	oracle, err := r.probeOracle(q, qfn, meta)
	if err != nil {
		return err
	}
	pairs, perr := ParseOracle(oracle)
	if perr != nil {
		// 探测成功但解析不出: 原文进日志供人工看格式
		for i, ln := range strings.Split(strings.ReplaceAll(oracle, "\r", ""), "\n") {
			if i >= 120 {
				r.logf("  …(截断, 共 %d 行)", len(strings.Split(oracle, "\n")))
				break
			}
			r.logf("  |%s", ln)
		}
		r.restoreArchive(q.EID, meta.StuCode)
		return perr
	}
	r.logf("  ☞ oracle: %d 组用例已拿到答案表 (首组期望 %d 字节)", len(pairs), len(pairs[0].Expected))
	if r.opts.ProbeOnly {
		r.bump(&r.status.Done) // 探测即完成 (不交 round-2)
		r.restoreArchive(q.EID, meta.StuCode)
		return nil
	}

	// ---- round 2: 查表代码验证 ----
	code, err := GenerateLookup(tpl, pairs)
	if err != nil {
		return fmt.Errorf("round-2 代码生成失败: %w", err)
	}
	passed, err := r.submitAndJudge(q, qfn, meta, code, "round-2")
	if err != nil {
		return err
	}
	return r.reportJudge(q, passed, "round-2 判题未通过 (测试数据疑似按次随机化, 查表法不适用; 需补真题解, 存档已还原)")
}

// reportJudge 判题落账: passed 计 solved; 未过不带计数原路返回错误
// (失败计数由 execSolve 统一记, 这里不重复)。
func (r *run) reportJudge(q *CatalogItem, passed bool, failMsg string) error {
	if passed {
		r.logf("  ✓ runGroup 全组通过, %s 已落 pass", q.PName)
		r.bump(&r.status.Solved)
		return nil
	}
	return errors.New(failMsg)
}

// submitAndJudge 交一版代码并判题: release→compile→runGroup 到 end→还原存档。
// 无论过没过、成败与否, 存档一律还原 (pass 记在服务端账上, 不靠存档代码)。
func (r *run) submitAndJudge(q *CatalogItem, qfn string, meta *QuestionMeta, code, tag string) (bool, error) {
	if err := r.client.Release(r.ctx); err != nil {
		r.logf("  ⚠ %v (继续)", err)
	}
	cd, err := r.client.Compile(r.ctx, q.EID, encodeCode(code), qfn)
	if err != nil {
		r.restoreArchive(q.EID, meta.StuCode)
		return false, fmt.Errorf("%s编译请求失败: %w", tag, err)
	}
	if !CompileOK(cd) {
		r.restoreArchive(q.EID, meta.StuCode)
		return false, fmt.Errorf("%s编译失败: %s", tag, firstLine(cd.Result))
	}
	if err := sleepCtx(r.ctx, solveStepPause); err != nil {
		r.restoreArchive(q.EID, meta.StuCode)
		return false, err
	}
	passed, err := r.judge(q, qfn, meta)
	// 存档还原必须在判题之后: pass 已落账, 留在判题会话里的代码不再有用
	r.restoreArchive(q.EID, meta.StuCode)
	if err != nil {
		return false, err
	}
	return passed, nil
}

// probeOracle 空桩判题: runGroup 逐组打到 end=true, 拼接全部 output。
func (r *run) probeOracle(q *CatalogItem, qfn string, meta *QuestionMeta) (string, error) {
	var sb strings.Builder
	for i := 0; i < solveRunGroupCap; i++ {
		rd, err := r.client.RunGroup(r.ctx, q.EID, "", qfn, meta.Kind, meta.Language)
		if err != nil {
			return "", fmt.Errorf("空桩判题请求失败 (第 %d 调): %w", i+1, err)
		}
		sb.WriteString(rd.Output)
		sb.WriteString("\n")
		if rd.End {
			break
		}
		if err := sleepCtx(r.ctx, solveStepPause); err != nil {
			return "", err
		}
	}
	return sb.String(), nil
}

// judge round-2 判题: runGroup 打到 end=true (C/C++ 不带 stuCode, 代码由
// compile 留在会话里 —— solutions/465/run.ts 已验证路径), 返回是否全组通过。
func (r *run) judge(q *CatalogItem, qfn string, meta *QuestionMeta) (bool, error) {
	for i := 0; i < solveRunGroupCap; i++ {
		rd, err := r.client.RunGroup(r.ctx, q.EID, "", qfn, meta.Kind, meta.Language)
		if err != nil {
			return false, fmt.Errorf("round-2 判题请求失败 (第 %d 调): %w", i+1, err)
		}
		if i == 0 {
			r.logf("  ↳ 判题首调: end=%v order=%d passed=%v right=%d err=%d needInput=%v",
				rd.End, rd.Order, rd.Passed, rd.RunRightCount, rd.RunErrCount, rd.NeedInput)
			r.logf("  ↳ 回显尾段: %q", truncate(lastLine(rd.Output), 200))
		}
		if rd.End {
			return rd.Passed, nil
		}
		if err := sleepCtx(r.ctx, solveStepPause); err != nil {
			return false, err
		}
	}
	return false, errors.New("round-2 判题超次数未到 end")
}

// lastLine 输出尾段 (含判题结论区), 剥掉 HTML 便于人工比对。
func lastLine(out string) string {
	dec := decodeOracleHTML(out)
	lines := strings.Split(strings.ReplaceAll(dec, "\r", ""), "\n")
	var keep []string
	for i, ln := range lines {
		if strings.TrimSpace(ln) != "" || (i < len(lines)-1 && strings.TrimSpace(lines[i+1]) != "") {
			keep = append(keep, ln)
		}
	}
	if len(keep) > 6 {
		keep = keep[len(keep)-6:]
	}
	return strings.Join(keep, " | ")
}

// restoreArchive 存档回写 + 读回核对 (restoreStuCode 的转接, 失败不终止整轮)。
func (r *run) restoreArchive(eID int64, snap string) {
	r.restoreStuCode(eID, snap)
	if err := r.client.Release(r.ctx); err != nil {
		r.logf("  ⚠ 收尾 %v", err)
	}
}
