---
name: anyview-idle
description: 极简瑞士奶油风格待机刷课控制台；扁平、视觉优先、少文字、温润克制
colors:
  cream-canvas: "#FAF7F2"
  cream-card: "#FCFAF7"
  cream-sub: "#F4EDE3"
  hairline: "#E6DDD0"
  espresso: "#201B17"
  slate-brown: "#635C54"
  caption: "#9C9388"
  terracotta: "#C85A32"
  sage-green: "#4A6B53"
  amber: "#C4883B"
  error-rose: "#C93B2B"
typography:
  display:
    fontFamily: "Fragment Mono, ui-monospace, Menlo, Consolas, monospace"
    fontSize: "clamp(3.5rem, 8vw, 5.5rem)"
    fontWeight: 500
    lineHeight: 1.1
    letterSpacing: "-0.02em"
  headline:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Inter, 'PingFang SC', sans-serif"
    fontSize: "22px"
    fontWeight: 600
    lineHeight: 1.3
  body:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Inter, 'PingFang SC', sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.6
  tabular:
    fontFamily: "Fragment Mono, ui-monospace, monospace"
    fontSize: "14px"
    fontWeight: 500
    fontVariantNumeric: "tabular-nums"
rounded:
  sm: "6px"
  md: "10px"
  lg: "16px"
  full: "9999px"
---

# Design System: anyview-idle (Minimalist Swiss Cream)

## 1. Creative North Star: 极简瑞士奶油风 (Swiss Cream Minimalist)
- **视觉优先 (Visual-First)**: 告别生硬冗长的文字说教与黑底单调极简。使用优雅的圆环进度仪表、双端点滑轨、状态胶囊与指标卡片，一眼捕获全站运行态。
- **少文字 (Concise & Self-Explanatory)**: 控件自解释，移除大段说明，通过直观的视觉交互（如双端点区间滑动条、标签切片、数值徽章）完成操作。
- **温润扁平 (Warm & Flat)**: 摒弃俗气的发光和粗劣阴影，采用温暖细腻的奶油纸面色底（`#FAF7F2`）搭配温润深咖啡字（`#201B17`）与陶土朱砂点缀（`#C85A32`），呈现国际主义排版（Swiss Style）的秩序与克制美感。
- **极速流畅 (Butter-Smooth 60fps)**: 消除一切 DOM 抖动、全量重建与轮询重绘瓶颈，滑动条采用硬件加速指针捕获，日志采用增量滚动缓冲区。

## 2. 颜色系统 (Color System)
- **Cream Canvas `#FAF7F2`**: 温暖柔和的奶油底色，夜间或长时挂机温和护眼，告别生硬死白与压抑纯黑。
- **Cream Card `#FCFAF7` / `#FFFFFF`**: 高透气度卡片底色，搭配 1px 发丝边界 `#E6DDD0`。
- **Tonal Sub `#F4EDE3`**: 滑轨未选区间、次级按钮底色、标签填充。
- **Primary Espresso `#201B17`**: 深度炭咖色主文字，沉稳锐利。
- **Secondary Slate Brown `#635C54`**: 次级说明、图例文本。
- **Terracotta Accent `#C85A32`**: 陶土朱砂，用于双端点滑轨激活线段、主控按钮与关键进度。
- **Sage Green `#4A6B53`**: 运行中呼吸点与正常完成状态。
- **Soft Amber `#C4883B`**: 换题间隙、等待状态。
- **Error Rose `#C93B2B`**: 错误回显与警示。

## 3. 设计小巧思: 双端点时长滑动条 (Dual-Endpoint Range Slider)
- 一条纤细平整的导轨，其上分布两枚圆形端点（下限 & 上限）。
- 左右端点拖拽时，两端点之间的区间以陶土暖色高亮贯通，实时计算并显示浮动分值（例如 `5.0m - 10.0m`）。
- 支持指针事件捕捉（Desktop Mouse + Touch），点击轨道任意位置自动吸附移动最近端点。
- 智能防重叠保护（下限不得超过上限，且自动保留最小安全步长）。
- 辅以快捷预设 Pill（`[3-6m]`, `[5-10m 常用]`, `[10-20m]`, `[15-30m]`），点击即刻平滑同步。

## 4. 视觉优先运行仪表 (Live Visual Execution Monitor)
- **进度圆环 (Circular Dial)**: 优雅的 SVG 双色圆环，实时动态展示当前题目耗时比例。
- **等宽大计时 (Tabular Tab)**: 毫秒级防抖动等宽数字，优雅律动。
- **4 联指标看板 (Quad Metrics Grid)**:
  - 已完成 (Done)
  - 自动跳过 (Skipped)
  - 模拟错误注入 (Errors)
  - 累计增涨时长 (Time Gained)
- **轻量夜录 (Audit Stream)**:
  - 增量追加，滚动自适应，支持一键清空与复制，彻底杜绝掉帧卡顿。
