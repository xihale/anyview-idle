<script setup>
import { ref, computed, onMounted } from "vue";
import { api, theme, SHOT } from "./api";
import LoginPanel from "./components/LoginPanel.vue";
import ConfigPanel from "./components/ConfigPanel.vue";
import RunPanel from "./components/RunPanel.vue";
import DonePanel from "./components/DonePanel.vue";
import LogPanel from "./components/LogPanel.vue";

const status = ref({});
const who = ref("");
const courses = ref([]);
const stopping = ref(false);
const preferCfg = ref(false);
// shot 截图模式直接以"刷完自动下线"视角展示小结页
const autoOff = ref(SHOT);
const hasLoadedCourses = ref(false);

const themeIcon = computed(() => (theme.mode === "auto" ? "Monitor" : theme.mode === "light" ? "Sunny" : "Moon"));
const stage = computed(() => {
  const st = status.value || {};
  if (!st.authenticated && !who.value) {
    const hadRun = !!(st.finishedAt || (st.log && st.log.length));
    return autoOff.value && hadRun ? "done" : "login";
  }
  if (st.running) return "run";
  const hadRun = !!(st.startedAt || st.finishedAt || (st.log && st.log.length) || st.done || st.solved || st.gainedSec);
  return !preferCfg.value && hadRun ? "done" : "cfg";
});

const stateChip = computed(() => {
  const st = status.value || {};
  if (st.running && stopping.value) return { text: "收尾中", type: "warning" };
  if (st.running) return { text: "运行中", type: "success" };
  if (st.error) return { text: "出错", type: "danger" };
  if (who.value && !preferCfg.value && st.finishedAt) return { text: "完成", type: "success" };
  if (who.value) return { text: "空闲", type: "info" };
  return { text: "未登录", type: "info" };
});

async function poll() {
  try {
    const st = await api("/api/status");
    status.value = st;
    if (st.authenticated) {
      who.value = st.student || who.value || "在线";
      if (!hasLoadedCourses.value) {
        hasLoadedCourses.value = true;
        try { courses.value = await api("/api/courses"); } catch {}
      }
    } else if (who.value) {
      who.value = "";
      autoOff.value = true;
      hasLoadedCourses.value = false;
    }
  } catch {}
  if (!SHOT) setTimeout(poll, 2000);
}

onMounted(poll);

async function onLoggedIn(student) {
  who.value = student || "在线";
  autoOff.value = false;
  preferCfg.value = true;
  hasLoadedCourses.value = true;
  try { courses.value = await api("/api/courses"); } catch {}
  status.value = { ...status.value, authenticated: true, running: false, student: who.value };
}

async function onLogout() {
  who.value = "";
  preferCfg.value = false;
  stopping.value = false;
  autoOff.value = false;
  hasLoadedCourses.value = false;
  try { await api("/api/logout", { method: "POST" }); } catch {}
  status.value = { running: false, authenticated: false, log: [] };
}

function onStopped() {
  stopping.value = true;
}
function onBack() {
  preferCfg.value = true;
}
function refreshStatus() {
  return poll();
}
</script>

<template>
  <header class="appbar">
    <div class="appbar-inner">
      <div class="brand">
        <el-icon class="logo" :size="26"><TrendCharts /></el-icon>
        <h1>anyview-idle</h1>
        <el-tag v-if="who" type="info" round effect="light" title="点击下线并清除本机会话" style="cursor:pointer;" @click="onLogout">{{ who }}</el-tag>
      </div>
      <el-tag :type="stateChip.type" round effect="light">
        <span style="display:inline-flex;align-items:center;gap:6px;">
          <span class="chip-dot" />{{ stateChip.text }}
        </span>
      </el-tag>
      <el-tooltip content="主题: 自动 / 浅色 / 深色" placement="bottom">
        <el-button text circle :icon="themeIcon" aria-label="切换主题" @click="theme.cycle()" />
      </el-tooltip>
    </div>
  </header>

  <main class="container">
    <el-alert
      v-if="status.error"
      type="error"
      :title="status.error"
      :closable="true"
      show-icon
    >
      <template v-if="/119|token|失效|未登录|顶掉/i.test(status.error || '')" #default>
        {{ status.error }}
        <el-button type="primary" text size="small" @click="onLogout">重新登录</el-button>
      </template>
    </el-alert>

    <LoginPanel v-if="stage === 'login'" @logged-in="onLoggedIn" />

    <template v-else-if="stage === 'cfg'">
      <ConfigPanel :courses="courses" :status="status" @started="refreshStatus" />
    </template>

    <template v-else-if="stage === 'run'">
      <RunPanel :status="status" :stopping="stopping" @stop="onStopped" @refresh="refreshStatus" />
    </template>

    <template v-else>
      <DonePanel :status="status" @back="onBack" />
    </template>

    <LogPanel :status="status" />
  </main>

  <footer class="page">任务结束自动下线让位 · token 失效约 3 小时后自动重登续跑</footer>
</template>

<style scoped>
.chip-dot { width: 8px; height: 8px; border-radius: 50%; background: currentColor; }
</style>
