package anyview

import (
	"encoding/json"
	"testing"
	"unicode/utf8"
)

func coursesFrom(t *testing.T, payload string) []Course {
	t.Helper()
	var env envelope
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		t.Fatal(err)
	}
	var data any
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatal(err)
	}
	var out []Course
	for _, row := range findRows(data, 0) {
		m := asMap(row)
		if m == nil {
			continue
		}
		if c, ok := rowToCourse(m); ok {
			out = append(out, c)
		}
	}
	return out
}

func TestNormalizeCourses_FlatRows(t *testing.T) {
	got := coursesFrom(t, `{"code":200,"message":"ok","data":{"rows":[
		{"courseId":11,"courseName":"数据结构","schemeId":465,"classId":381,"termName":"2025-2026-1"},
		{"schemeId":435,"classId":348}
	]}}`)
	if len(got) != 2 {
		t.Fatalf("want 2 courses, got %d: %+v", len(got), got)
	}
	if got[0].CourseName != "数据结构" || got[0].SchemeID != 465 || got[0].ClassID != 381 || got[0].TermName != "2025-2026-1" {
		t.Errorf("flat row 解析错: %+v", got[0])
	}
	if got[1].CourseName != "课程 435" {
		t.Errorf("缺名兜底错: %+v", got[1])
	}
}

func TestNormalizeCourses_NestedCourseClass(t *testing.T) {
	got := coursesFrom(t, `{"code":200,"message":"ok","data":{"list":[
		{"course":{"id":7,"cname":"C 语言","schemeId":465},"class":{"id":99}}
	]}}`)
	if len(got) != 1 {
		t.Fatalf("want 1 course, got %d: %+v", len(got), got)
	}
	c := got[0]
	if c.CourseName != "C 语言" || c.SchemeID != 465 || c.ClassID != 99 || c.CourseID != 7 {
		t.Errorf("嵌套结构解析错: %+v", c)
	}
}

func TestNormalizeCourses_TopLevelArrayAndStrings(t *testing.T) {
	got := coursesFrom(t, `{"code":31,"message":"ok","data":[
		{"name":"计组","schemeId":"465","stuClassId":"381","semester":"2026春"}
	]}`)
	if len(got) != 1 {
		t.Fatalf("want 1 course, got %d: %+v", len(got), got)
	}
	c := got[0]
	if c.CourseName != "计组" || c.SchemeID != 465 || c.ClassID != 381 || c.TermName != "2026春" {
		t.Errorf("字符串数字解析错: %+v", c)
	}
}

func TestNormalizeCourses_SchemeListRows(t *testing.T) {
	// 2026 后端 /scheme-service/scheme/list/student 实测行形状
	got := coursesFrom(t, `{"code":31,"message":"","data":[
		{"id":465,"name":"数据结构(C语言版)(计算机)-2026秋","courseId":20,
		 "courseName":"数据结构(C语言版)","classId":381,"passNum":5},
		{"id":466,"name":"数据结构实验(C语言版)(计算机)-2026秋","courseId":51,
		 "courseName":"数据结构实验(C语言版)","classId":381}
	]}`)
	if len(got) != 2 {
		t.Fatalf("want 2 courses, got %d: %+v", len(got), got)
	}
	if got[0].SchemeID != 465 || got[0].ClassID != 381 || got[0].CourseID != 20 {
		t.Errorf("scheme 行解析错: %+v", got[0])
	}
	if got[0].CourseName != "数据结构(C语言版)(计算机)-2026秋" {
		t.Errorf("应优先用全名 name: %+v", got[0])
	}
}

func TestTruncateUTF8Boundary(t *testing.T) {
	s := "你好世界" // 每字 3 bytes
	got := truncate(s, 4)
	if got[len(got)-len("…"):] != "…" {
		t.Fatalf("应带省略号, got %q", got)
	}
	if !utf8.ValidString(got[:len(got)-len("…")]) {
		t.Fatalf("截断不应切开 rune, got %q", got)
	}
}

func TestNormalizeCourses_IncompleteRowDropped(t *testing.T) {
	got := coursesFrom(t, `{"code":200,"message":"ok","data":[
		{"courseName":"没有 schemeId"},
		{"schemeId":1,"classId":2}
	]}`)
	if len(got) != 1 || got[0].SchemeID != 1 {
		t.Fatalf("应丢弃缺 ID 的行: %+v", got)
	}
}
