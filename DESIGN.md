---
name: anyview-idle
description: 「anyview-idle」控制台；status-lite 设计语言——MD3 teal 双主题、卡片、少字一眼读状态
colors:
  primary: "#00696b"
  onPrimary: "#ffffff"
  primaryContainer: "#9cf1f0"
  onPrimaryContainer: "#002021"
  surface: "#f4faf9"
  onSurface: "#161d1d"
  onSurfaceVariant: "#3f4949"
  surfaceLow: "#eef4f4"
  surfaceHighest: "#dde3e3"
  outline: "#6f7979"
  outlineVariant: "#bec8c7"
  error: "#ba1a1a"
  warn: "#7c5800"
typography:
  body:
    fontFamily: "system-ui, Roboto, 'Noto Sans SC', 'PingFang SC', sans-serif"
    fontSize: "14px"
    lineHeight: 1.5
  metric:
    fontSize: "44px"
    fontWeight: 400
    fontVariantNumeric: "tabular-nums"
  cardTitle:
    fontSize: "14px"
    fontWeight: 600
    letterSpacing: "0.4px"
  log:
    fontFamily: "ui-monospace, Menlo, Consolas, monospace"
    fontSize: "12px"
shape:
  cardRadius: "12px"
  pillRadius: "9999px"
motion:
  emphasized: "cubic-bezier(0.2, 0, 0, 1)"
---

# Design System: anyview-idle（status-lite 设计语言）

## 1. Creative North Star

设计语言整体取自 `~/Desktop/learning/go/status-lite`（MD3 teal 种子配色、单文件内嵌、
卡片即界面），通过 Element Plus 的 `--el-*` 变量整棵灌入组件观感。要的是优雅、简洁、
没有 AI slop：无渐变、无玻璃拟态堆砌、无多余动效。

- **卡片即界面**：内容都住在 `surface-container-low`（#eef4f4 / 深色 #161d1d）的 12px
  圆角卡里，无阴影无描边，进场一次性 `translateY(8px)` 淡入。
- **teal 只表意图**：主色 #00696b（深色 #4ddadb）只给主按钮、选中态、进度、卡片图标；
  warn 琥珀 / error 红只做语义。
- **一眼读状态**：运行态主角是 44px tabular-nums 大数字（挂到几分 / 过了几题），
  顶栏一颗 chip 说全局状态（运行中/空闲/出错/完成/未登录）。
- **少字**：控件自带标签，说明一行说尽（如"仅探测 · 只跑空桩拿答案表，不改 pass"）。

## 2. 主题

`html.dark` 承载深色方案（Element 约定），auto/light/dark 三态循环按钮 +
`localStorage anv-theme`。浅深两套 token 都在 `web-src/src/styles.css`
（MD3 `--md-*` + Element `--el-*` 映射，Element 的 dark css-vars 之后覆写）。

## 3. 组件词汇

- 卡片头：20px 主色描边图标 + 14px/600 标题 + 右侧 `el-tag round` 状态 chip。
- 大指标：`.metric-big`（44px 数字 + 18px 灰单位）。
- 进度：`.bar-track/.bar-fill`（8px 全圆角，muted=跳过、err=失败的分段栈）。
- 区间输入：一对无边框控制钮的 `el-input-number` + `el-check-tag` 预设（选中=teal 容器色）。
- 任务类型：`el-radio-group` 按钮组（挂机计时 / 自动刷题），切模式换配置卡。
- 日志：12px mono 终端块，滚动上限 200 行，复制全文 + 滚屏开关。

## 4. 四态

1. **登录**：学号、密码；Token / XFF 折在 collapse 里。
2. **配置**：课程 → 任务类型 →（时长/注入 | 仅探测）→ 题号范围 → 开始。
3. **运行**：当前题 + 大数字进度 + 盘点统计 + 分段总进度 + 停止。
4. **小结**：大数字（涨幅分钟 | 通过题数）+ ElDescriptions 盘点 + 返回配置。

## 5. 运行日志

日志是审计线索，保持可读可复制；`?shot=1&stage=…` 用内置夹具离线截图验收。
