package anyview

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---------- oracle 解析 ----------

// 真实样本: 2026-09-20 DC01PE21 (11428663) 判题回显原文, 逐字节保留。
const realOracleOne = "函数输入：<br>Score&nbsp;of&nbsp;student[6]&nbsp;=&nbsp;{&nbsp;17.76,42.86,24.90,19.65,11.04,73.88&nbsp;}<br><br>你的结果：<br>0<br><br>系统结果：<br>73.87<br>--------ERROR--------<br><br>\n"

func TestParseOracleRealFormat(t *testing.T) {
	pairs, err := ParseOracle(realOracleOne)
	if err != nil {
		t.Fatalf("ParseOracle: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("want 1 pair, got %d: %+v", len(pairs), pairs)
	}
	want := OraclePair{
		Input:    "Score of student[6] = { 17.76,42.86,24.90,19.65,11.04,73.88 }",
		Expected: "73.87\n",
	}
	if pairs[0] != want {
		t.Errorf("pair = %+v, want %+v", pairs[0], want)
	}
}

func TestParseOracleMultiGroup(t *testing.T) {
	// solver 逐组调 runGroup 后拼接: 每组一段、各带一个判题标记
	multi := realOracleOne +
		"函数输入：<br>-1&nbsp;2<br><br>你的结果：<br>0<br><br>系统结果：<br>ERROR<br>--------ERROR--------<br><br>\n" +
		"函数输入：<br>10&nbsp;20<br><br>你的结果：<br>30<br><br>系统结果：<br>30<br>--------RIGHT--------<br><br>\n"
	pairs, err := ParseOracle(multi)
	if err != nil {
		t.Fatalf("ParseOracle: %v", err)
	}
	want := []OraclePair{
		{Input: "Score of student[6] = { 17.76,42.86,24.90,19.65,11.04,73.88 }", Expected: "73.87\n"},
		{Input: "-1 2", Expected: "ERROR\n"},
		{Input: "10 20", Expected: "30\n"},
	}
	if len(pairs) != len(want) {
		t.Fatalf("want %d pairs, got %d: %+v", len(want), len(pairs), pairs)
	}
	for i := range want {
		if pairs[i] != want[i] {
			t.Errorf("pair[%d] = %+v, want %+v", i, pairs[i], want[i])
		}
	}
}

func TestParseOracleEqualsMarkers(t *testing.T) {
	// 等号包围的旧渲染标记 (===ERROR=== / ---RIGHT---), 布局同现网: 标记在系统结果之后
	sample := "函数输入：<br>3&nbsp;5<br><br>你的结果：<br>0<br><br>系统结果：<br>8<br>===ERROR===<br><br>\n" +
		"函数输入：<br>10&nbsp;20<br><br>你的结果：<br>30<br><br>系统结果：<br>30<br>---RIGHT---<br><br>\n"
	pairs, err := ParseOracle(sample)
	if err != nil {
		t.Fatalf("ParseOracle: %v", err)
	}
	want := []OraclePair{{Input: "3 5", Expected: "8\n"}, {Input: "10 20", Expected: "30\n"}}
	if len(pairs) != len(want) {
		t.Fatalf("want %d pairs, got %d: %+v", len(want), len(pairs), pairs)
	}
	for i := range want {
		if pairs[i] != want[i] {
			t.Errorf("pair[%d] = %+v, want %+v", i, pairs[i], want[i])
		}
	}
}

func TestParseOracleGarbage(t *testing.T) {
	if _, err := ParseOracle("编译失败, 无输出"); err == nil {
		t.Fatal("无标签输出应报错")
	}
}

// ---------- 函数定位 ----------

const sampleTemplate = `#include <stdio.h>
#include <stdlib.h>

typedef struct TNode {
    int val;
    struct TNode *l, *r;
} BiTree;

/* 在这里补全: 返回 a 与 b 之和; k<0 视为非法 */
int addUp(int a, int b) {
}

int main(void) {
    int a, b;
    scanf("%d %d", &a, &b);
    printf("%d\n", addUp(a, b));
    return 0;
}
`

func TestFindTargetFunc(t *testing.T) {
	f, err := findTargetFunc(sampleTemplate)
	if err != nil {
		t.Fatalf("findTargetFunc: %v", err)
	}
	if f.Name != "addUp" {
		t.Fatalf("target = %s, want addUp", f.Name)
	}
	if !f.BodyIsStub {
		t.Fatal("addUp 应识别为空桩")
	}
}

func TestFindTargetFuncCommentBody(t *testing.T) {
	tpl := strings.Replace(sampleTemplate, "int addUp(int a, int b) {\n}", "int addUp(int a, int b) {\n    // TODO\n}", 1)
	f, err := findTargetFunc(tpl)
	if err != nil || f.Name != "addUp" {
		t.Fatalf("注释体空桩未识别: %v %+v", err, f)
	}
}

// 真实模板形态 (DC01PE21, 2026-09-20 实拉): 块注释夹在签名与 '{' 之间,
// 函数体是 "// Add your code here" + 空白。
const realShapeTemplate = `#include <stdio.h>
#include <stdlib.h>

typedef struct { float s; } stuType;

float highestScore(stuType *student[], int n)
/* 返回最高成绩  */
{  // Add your code here

    

}

int main(void) {
    stuType a; a.s = 3.5f;
    stuType *p[1] = {&a};
    printf("%.2f\n", highestScore(p, 1));
    return 0;
}
`

func TestFindTargetFuncRealTemplate(t *testing.T) {
	f, err := findTargetFunc(realShapeTemplate)
	if err != nil {
		t.Fatalf("findTargetFunc: %v", err)
	}
	if f.Name != "highestScore" {
		t.Fatalf("target = %s, want highestScore", f.Name)
	}
	if !f.BodyIsStub {
		t.Fatal("注释+空白体应识别为空桩")
	}
}

func TestGenerateLookupRealTemplate(t *testing.T) {
	pairs := []OraclePair{
		{Input: "Score of student[6] = { 17.76,42.86,24.90,19.65,11.04,73.88 }", Expected: "73.87\n"},
	}
	code, err := GenerateLookup(realShapeTemplate, pairs)
	if err != nil {
		t.Fatalf("GenerateLookup: %v", err)
	}
	// 平台附加自己的 main: 函数补全路径绝不能再生成一个 main
	if strings.Count(code, "int main") != 1 {
		t.Fatalf("生成代码应只保留模板原有的 1 个 main:\n%s", code)
	}
	if !strings.Contains(code, "float highestScore(stuType *student[], int n)") {
		t.Fatal("原函数签名丢失")
	}
	in := "Score of student[6] = { 17.76,42.86,24.90,19.65,11.04,73.88 }\n"
	if got := buildAndRun(t, code, in); got != "73.87\n" {
		t.Errorf("got %q, want 73.87 newline-terminated", got)
	}
}

// ---------- 代码生成 + 本地 gcc 行为验证 ----------

func buildAndRun(t *testing.T, code, input string) string {
	t.Helper()
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("no gcc")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "gen.cpp")
	exe := filepath.Join(dir, "gen")
	if err := os.WriteFile(src, []byte(code), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("gcc", src, "-o", exe).CombinedOutput(); err != nil {
		t.Fatalf("gcc: %v\n%s\ncode:\n%s", err, out, code)
	}
	cmd := exec.Command(exe)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return string(out)
}

func TestGenerateLookupFunctionCompletion(t *testing.T) {
	pairs := []OraclePair{
		{Input: "3 5", Expected: "8\n"},
		{Input: "-1 2", Expected: "ERROR\n"},
		{Input: "10 20", Expected: "30\n"},
	}
	code, err := GenerateLookup(sampleTemplate, pairs)
	if err != nil {
		t.Fatalf("GenerateLookup: %v", err)
	}
	// 计数器查表: 每组一次程序执行、第 k 次调用打印第 k 组输出。
	// 每个用例都是新进程 → 首调用 → vals[0], 与喂什么 stdin 无关。
	for _, in := range []string{"3 5\n", "-1 2\n", "10 20\n", "3  5"} {
		if got := buildAndRun(t, code, in); got != "8\n" {
			t.Errorf("input %q: got %q, want 8", in, got)
		}
	}
	if !strings.Contains(code, "anv_vals") || !strings.Contains(code, "int addUp(int a, int b)") {
		t.Fatal("生成代码缺值表或原函数签名")
	}
}

func TestGenerateLookupWholeProgram(t *testing.T) {
	tpl := `#include <stdio.h>
int main(void) {
    int x;
    scanf("%d", &x);
    printf("%d\n", x * 2);
    return 0;
}
`
	pairs := []OraclePair{{Input: "21", Expected: "42\n"}}
	code, err := GenerateLookup(tpl, pairs)
	if err != nil {
		t.Fatalf("GenerateLookup: %v", err)
	}
	if got := buildAndRun(t, code, "21\n"); got != "42\n" {
		t.Errorf("got %q, want 42", got)
	}
}

func TestCLiteralEscaping(t *testing.T) {
	if got := cLiteral("a\"b\\c\nd\x01"); got != `"a\"b\\c\nd\001"` {
		t.Errorf("cLiteral = %s", got)
	}
}
