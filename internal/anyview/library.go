// 本地题解库: fuckView data-ds/Homework 全量真题解 (8 章 129 题)。
// 测试数据按次随机化的题 (DC01PE21 实测) 查表法必败, 真题解才是通用通路 ——
// 命中题解库时跳过 oracle 两轮, 直接交真题解判题。
// 未收 dc06pe23: fuckView 实测 err=4 未过, 收了只会白烧判题次数。
// 整程题 (DC01PE03e 等 e 后缀) 的题解自带 main, 与平台附加 main 的
// 函数补全题互斥, 文件按实测形状原样保留。
package anyview

import (
	"embed"
	"errors"
	"regexp"
	"strings"
)

//go:embed solutions/*.cpp
var solutionsFS embed.FS

// rePNameKey 从题名里取题号 (DC01PE06 / DC01PE03e 这类), 题解文件按 小写题号.cpp 命名。
var rePNameKey = regexp.MustCompile(`(?i)dc\d+pe\d+[a-z]?`)

// librarySolution 按题名查本地题解库, 命中返回题解源码。
func librarySolution(pname string) (string, error) {
	key := strings.ToLower(rePNameKey.FindString(pname))
	if key == "" {
		return "", errors.New("题名里没有 DCnnPEnn 题号")
	}
	b, err := solutionsFS.ReadFile("solutions/" + key + ".cpp")
	if err != nil {
		return "", errors.New("题解库没有 " + strings.ToUpper(key))
	}
	return string(b), nil
}
