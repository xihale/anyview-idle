package anyview

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDecodeCode_Base64Archive(t *testing.T) {
	src := "int main() {\n    return 0;\n}\n"
	got := decodeCode(base64.StdEncoding.EncodeToString([]byte(src)))
	if got != src {
		t.Fatalf("b64 存档应还原为源码, got %q", got)
	}
}

func TestDecodeCode_PlainFallback(t *testing.T) {
	src := "int main() {\n}\n" // 带换行, 不是合法 b64 → 按原文透传
	if got := decodeCode(src); got != src {
		t.Fatalf("非 b64 存档应原文透传, got %q", got)
	}
	if got := decodeCode(""); got != "" {
		t.Fatalf("空存档应返回空, got %q", got)
	}
}

func TestDecodeCode_BinaryRejected(t *testing.T) {
	// "main" 是合法 b64, 但解出二进制垃圾 → 应回退原文
	junk := base64.StdEncoding.EncodeToString([]byte{0x00, 0x01, 0x02})
	_ = junk
	got := decodeCode("///") // 合法 b64 字符但解码含控制字节
	if got != "///" {
		t.Fatalf("二进制解码结果应回退原文, got %q", got)
	}
}

func TestCorruptCode_RemovesSemicolon(t *testing.T) {
	code := "int x = 1;\nint y = 2;\n"
	got := corruptCode(code)
	if strings.Contains(got, "int x = 1;") {
		t.Fatalf("应删掉第一个分号, got %q", got)
	}
	if !strings.Contains(got, "int y = 2;") {
		t.Fatalf("其余内容应保留, got %q", got)
	}
}

func TestCorruptCode_TruncatesWithoutSemicolon(t *testing.T) {
	code := strings.Repeat("a", 100)
	got := corruptCode(code)
	if len(got) != 70 {
		t.Fatalf("无分号应截 70%%, want 70 bytes, got %d", len(got))
	}
}

func TestCorruptCode_EmptyFallback(t *testing.T) {
	if got := corruptCode(""); got != "int ;\n" {
		t.Fatalf("空代码应用保底残码, got %q", got)
	}
}

func TestDrawCount(t *testing.T) {
	if drawCount(0, 0) != 0 {
		t.Fatal("0-0 应表示不注入")
	}
	if drawCount(3, 0) != 0 {
		t.Fatal("下限>上限 应视为不注入")
	}
	for i := 0; i < 200; i++ {
		n := drawCount(2, 4)
		if n < 2 || n > 4 {
			t.Fatalf("drawCount(2,4) 越界: %d", n)
		}
	}
}

func TestEncodeCodeTrims(t *testing.T) {
	got := encodeCode("  int x;\n ")
	decoded, err := base64.StdEncoding.DecodeString(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != "int x;" {
		t.Fatalf("encode 应 trim 后编码, got %q", decoded)
	}
}

func TestCalcInjectCount(t *testing.T) {
	// 1. 已在区间内：跳过
	for cur := 1; cur <= 3; cur++ {
		_, _, skip, reason := calcInjectCount(cur, 1, 3)
		if !skip {
			t.Fatalf("cur=%d 已经在 [1,3] 区间内应跳过, got skip=%v", cur, skip)
		}
		if !strings.Contains(reason, "区间内") {
			t.Fatalf("cur=%d reason 应含 '区间内', got %q", cur, reason)
		}
	}

	// 2. 超过上限：跳过
	for cur := 4; cur <= 10; cur++ {
		_, _, skip, reason := calcInjectCount(cur, 1, 3)
		if !skip {
			t.Fatalf("cur=%d 超过上限应跳过, got skip=%v", cur, skip)
		}
		if !strings.Contains(reason, "上限") {
			t.Fatalf("cur=%d reason 应含 '上限', got %q", cur, reason)
		}
	}

	// 3. 未达下限：随机到区间内并补足差额
	for i := 0; i < 100; i++ {
		target, need, skip, _ := calcInjectCount(0, 1, 3)
		if skip {
			t.Fatalf("cur=0 不在 [1,3] 内不应跳过")
		}
		if target < 1 || target > 3 {
			t.Fatalf("target 应在 [1,3] 内, got %d", target)
		}
		if need != target {
			t.Fatalf("cur=0 need 应等于 target, got need=%d, target=%d", need, target)
		}
	}

	// 4. cur=1, 区间 [2, 4]: target 在 [2,4], need = target - 1
	for i := 0; i < 100; i++ {
		target, need, skip, _ := calcInjectCount(1, 2, 4)
		if skip {
			t.Fatalf("cur=1 不在 [2,4] 内不应跳过")
		}
		if target < 2 || target > 4 {
			t.Fatalf("target 应在 [2,4] 内, got %d", target)
		}
		if need != target-1 {
			t.Fatalf("cur=1 need 应为 target-1, got need=%d target=%d", need, target)
		}
		if 1+need != target {
			t.Fatalf("现有 + 补足 应刚好等于 target, got %d + %d != %d", 1, need, target)
		}
	}

	// 5. 注入未开启 (hi <= 0)
	_, _, skip, _ := calcInjectCount(0, 0, 0)
	if !skip {
		t.Fatal("0-0 应跳过")
	}
}
