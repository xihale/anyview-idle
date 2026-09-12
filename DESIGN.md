---
name: anyview-idle
description: 「无事」夜色待机控制台；扁平、少字、一眼读状态
colors:
  ink: "#141310"
  paper: "#EAE5DA"
  gray: "#A29B8B"
  hairline: "#2E2B25"
  cinnabar: "#D2604A"
typography:
  display:
    fontFamily: "Fragment Mono, ui-monospace, Menlo, Consolas, monospace"
    fontSize: "clamp(4.5rem, 18vw, 7rem)"
    fontWeight: 500
    lineHeight: 0.88
    letterSpacing: "-0.04em"
  summary:
    fontFamily: "Fragment Mono, ui-monospace, monospace"
    fontSize: "clamp(2.5rem, 10vw, 4rem)"
    fontWeight: 500
  target:
    fontFamily: "Fragment Mono, ui-monospace, monospace"
    fontSize: "clamp(1.25rem, 4vw, 1.75rem)"
    fontWeight: 400
  gap:
    fontFamily: "Fragment Mono, ui-monospace, monospace"
    fontSize: "1.5rem"
  body:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', sans-serif"
    fontSize: "15px"
    fontWeight: 400
    lineHeight: 1.5
  small:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', sans-serif"
    fontSize: "14px"
  caption:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', sans-serif"
    fontSize: "13px"
  log:
    fontFamily: "Fragment Mono, ui-monospace, monospace"
    fontSize: "12px"
  tabular:
    fontFamily: "Fragment Mono, ui-monospace, monospace"
    fontSize: "16px"
    fontWeight: 500
    fontVariantNumeric: "tabular-nums"
rounded:
  none: "0"
---

# Design System: anyview-idle（无事）

## 1. Creative North Star

工具在工作时应当看起来像什么都没发生。页面只给出当前这一刻：登录、配置、运行、小结，同一栏，不用卡片堆叠。夜间书桌、余光扫一眼——呼吸点说是否在跑，巨大分钟数字说挂到哪了。

- **无事**: 没有卡片、没有圆角、没有阴影、没有发光。分组靠留白和发丝线。
- **一眼**: 运行态的主角是等宽大数字，旁边是安静的目标值。
- **少字**: 控件自带标签；说明只留两行产品约束。
- **朱砂只用一次**: `#D2604A` 只给运行中的呼吸点和错误，其余是墨底纸色。

## 2. 颜色

- **Ink `#141310`**: 画布。夜色，长时挂机不刺眼。
- **Paper `#EAE5DA`**: 主文字、主按钮底、完成态圆点。
- **Gray `#A29B8B`**: 标签、提示、空闲圆点描边。
- **Hairline `#2E2B25`**: 下划线与发丝边。
- **Cinnabar `#D2604A`**: 运行呼吸点、出错字。

## 3. 签名交互

- **呼吸点**: 顶栏 8px 圆点。空心 = 空闲/未登录；朱砂缓呼吸（6s）= 运行中；实心纸色 = 本轮完成。`prefers-reduced-motion` 时不呼吸。
- **大数字**: Fragment Mono 等宽分钟值，`→ 目标` 紧随其后。换题间隙数字静为破折号。
- **下划线输入**: 学号/密码/区间都是一条发丝线，焦点时线变纸色。

## 4. 四态

1. **登录**: 学号、密码；token / XFF 折在 details 里。
2. **配置**: 课程、每题时长、错误注入、题号。两行约束说明。开始。
3. **运行**: 题名一行、大数字、一行盘点、停止（当前题挂完即停）。
4. **小结**: 本轮涨幅与盘点，返回配置。夜录仍在页脚。

## 5. 夜录

页脚 `夜录 · N 行`，默认折叠。打开后可复制。增量追加，不重绘整页。
