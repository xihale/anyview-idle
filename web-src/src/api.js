// API 封装 + 截图模式夹具。
// ?shot=1 时不出网, 全部走 fixtures —— 用于无账号环境下的 UI 截图验收。
export const SHOT = new URLSearchParams(location.search).get("shot") === "1";

const fixtures = {
  "/api/courses": [
    { courseName: "数据结构(编程)", schemeId: 465, classId: 381, termName: "2026秋" },
    { courseName: "概率论与数理统计", schemeId: 470, classId: 385, termName: "2026秋" },
  ],
  "/api/catalog": { total: 131, unpassed: 127 },
};

const statusFixtures = {
  login: { authenticated: false, running: false, log: [] },
  cfg: {
    authenticated: true, student: "韩亮", running: false,
    savedOptions: { mode: "idle", loMin: 5, hiMin: 10, injectLo: 1, injectHi: 3 },
    log: [],
  },
  "run-idle": {
    authenticated: true, student: "韩亮", running: true, mode: "idle",
    total: 127, done: 8, skipped: 3, failed: 1, gainedSec: 3960, injectedErrors: 5,
    current: {
      eid: 11428542, pname: "DC01PE11", chapName: "第2章",
      accumSec: 210, targetMin: 8.5, startedAt: new Date(Date.now() - 4 * 60000).toISOString(),
    },
    startedAt: new Date(Date.now() - 3 * 3600000).toISOString(),
    log: [
      "09:12:01  ▶ 开始: 区间 [5.0, 10.0] 分钟, scheme=465 class=381",
      "09:12:03  目录 131 题 (截取范围 131 题), 未通过 127 题为目标",
      "09:41:22  [9/127] DC01PE11 (11428542) 第2章 现累计 210s (3.5 分钟) → 挂 5.0 分钟到 8.5 分钟目标点",
    ],
  },
  "run-solve": {
    authenticated: true, student: "韩亮", running: true, mode: "solve",
    total: 127, done: 0, skipped: 0, failed: 2, solved: 6, gainedSec: 0, injectedErrors: 0,
    current: { eid: 11428542, pname: "DC01PE11", chapName: "第2章", accumSec: 0, targetMin: 0, startedAt: new Date().toISOString() },
    startedAt: new Date(Date.now() - 1800000).toISOString(),
    log: [
      "10:02:11  ▶ 自动刷题: scheme=465 class=381 (oracle 泄露两轮法)",
      "10:02:14  目录 131 题 (截取 131), 未通过且可判题的 127 题为目标",
      "10:03:40  [7/127] DC01PE07 (11428489) 第1章-DC01PE07 kind=1 lang=1",
      "10:04:02    ☞ oracle: 6 组用例已拿到答案表",
      "10:04:30    ✓ runGroup 全组通过, DC01PE07 已落 pass",
    ],
  },
  "done-idle": {
    authenticated: false, running: false, mode: "idle",
    total: 127, done: 11, skipped: 4, failed: 0, gainedSec: 6300, injectedErrors: 9,
    startedAt: new Date(Date.now() - 5 * 3600000).toISOString(),
    finishedAt: new Date(Date.now() - 600000).toISOString(),
    log: ["09:12:01  ▶ 开始", "13:44:52  ■ 全部目标处理完毕: 挂机 11, 跳过 4, 失败 0, 累计 +105.0 分钟, 注入错误 9 次"],
  },
  "done-solve": {
    authenticated: false, running: false, mode: "solve",
    total: 10, done: 0, skipped: 1, failed: 2, solved: 7, gainedSec: 0, injectedErrors: 0,
    startedAt: new Date(Date.now() - 2 * 3600000).toISOString(),
    finishedAt: new Date(Date.now() - 300000).toISOString(),
    log: ["11:00:01  ▶ 自动刷题", "12:58:33  ■ 刷题完毕: 通过 7, 跳过 1, 失败 2"],
  },
};

export async function api(path, opts) {
  if (SHOT) {
    if (path === "/api/status") {
      const want = new URLSearchParams(location.search).get("stage") || "login";
      return JSON.parse(JSON.stringify(statusFixtures[want] || statusFixtures.login));
    }
    await new Promise((r) => setTimeout(r, 120));
    return JSON.parse(JSON.stringify(fixtures[path] ?? {}));
  }
  const res = await fetch(path, opts);
  let body = {};
  try { body = await res.json(); } catch {}
  if (!res.ok) throw new Error(body.error || ("HTTP " + res.status));
  return body;
}

/* ---------- 主题: auto / light / dark 三态循环 ---------- */
export const theme = {
  mode: localStorage.getItem("anv-theme") || "auto",
  darkMq: matchMedia("(prefers-color-scheme: dark)"),
  apply() {
    const dark = this.mode === "dark" || (this.mode === "auto" && this.darkMq.matches);
    document.documentElement.classList.toggle("dark", dark);
    document.documentElement.dataset.theme = dark ? "dark" : "light";
  },
  cycle() {
    this.mode = this.mode === "auto" ? "light" : this.mode === "light" ? "dark" : "auto";
    localStorage.setItem("anv-theme", this.mode);
    this.apply();
  },
};
theme.darkMq.addEventListener("change", () => { if (theme.mode === "auto") theme.apply(); });
theme.apply();
