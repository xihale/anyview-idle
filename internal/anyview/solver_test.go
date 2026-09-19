package anyview

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---------- oracle 解析 ----------

const sampleOracle = `========RIGHT========
第1组
输入:
3 5
你的结果:
0 0
---ERROR---
系统结果:
8
===ERROR===
第2组
输入:
-1 2
你的结果:
0 0
---ERROR---
系统结果:
ERROR
===ERROR===
第3组
输入:
10 20
你的结果:
30
---RIGHT---
系统结果:
30
===RIGHT===
`

func TestParseOracle(t *testing.T) {
	pairs, err := ParseOracle(sampleOracle)
	if err != nil {
		t.Fatalf("ParseOracle: %v", err)
	}
	if len(pairs) != 3 {
		t.Fatalf("want 3 pairs, got %d: %+v", len(pairs), pairs)
	}
	want := []OraclePair{
		{Input: "3 5", Expected: "8"},
		{Input: "-1 2", Expected: "ERROR"},
		{Input: "10 20", Expected: "30"},
	}
	for i, w := range want {
		if pairs[i] != w {
			t.Errorf("pair[%d] = %+v, want %+v", i, pairs[i], w)
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
	cases := []struct{ in, want string }{
		{"3 5\n", "8\n"},
		{"-1 2\n", "ERROR\n"},
		{"10 20\n", "30\n"},
		{"3  5", "8\n"}, // 归一化空白: 依然命中
	}
	for _, c := range cases {
		if got := buildAndRun(t, code, c.in); got != c.want {
			t.Errorf("input %q: got %q, want %q", c.in, got, c.want)
		}
	}
	if !strings.Contains(code, "anv_sniff") || !strings.Contains(code, "int addUp(int a, int b)") {
		t.Fatal("生成代码缺 sniff 或原函数签名")
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
