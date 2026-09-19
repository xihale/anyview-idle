<script setup>
import { reactive, ref, computed, onMounted, watch } from "vue";
import { ElMessage } from "element-plus";
import { api } from "../api";

const props = defineProps({
  courses: { type: Array, default: () => [] },
  status: { type: Object, default: () => ({}) },
});
const emit = defineEmits(["started"]);

const courseId = ref("");
const qHint = ref("");
const qTotal = ref(0);
const starting = ref(false);

const form = reactive({
  mode: localStorage.getItem("anv-mode") || "idle",
  loDur: 5,
  hiDur: 10,
  loInj: 1,
  hiInj: 3,
  probeOnly: false,
  loLimit: 1,
  hiLimit: 1,
});

const validCourses = computed(() => props.courses.filter((c) => !(c.courseName || "").includes("实验")));
const courseKey = (c) => `${c.schemeId}:${c.classId}`;

const durationPresets = [
  { lo: 3, hi: 6, label: "3–6 轻度" },
  { lo: 5, hi: 10, label: "5–10 标准" },
  { lo: 10, hi: 20, label: "10–20 进阶" },
  { lo: 15, hi: 30, label: "15–30 深度" },
];
const injectPresets = [
  { lo: 0, hi: 0, label: "仅时长" },
  { lo: 1, hi: 3, label: "1–3 仿人推荐" },
  { lo: 3, hi: 6, label: "3–6 活跃" },
];

function setMode(m) {
  form.mode = m;
  localStorage.setItem("anv-mode", m);
}

function onCourseChange(key) {
  const c = validCourses.value.find((x) => courseKey(x) === key);
  if (!c) return;
  loadCatalog(c);
}

async function loadCatalog(c) {
  qHint.value = "读取目录…";
  try {
    const data = await api(`/api/catalog?schemeId=${c.schemeId}&classId=${c.classId}`);
    qTotal.value = Math.max(1, data.total || 1);
    form.hiLimit = qTotal.value;
    qHint.value = `共 ${qTotal.value} 题` + (data.unpassed !== undefined ? `，${data.unpassed} 题待刷（未通过）` : "");
  } catch {
    qTotal.value = 0;
    qHint.value = "";
    ElMessage.warning("目录暂时读不到，将按全部未通过题开刷。");
  }
}

function applySaved(opts) {
  if (!opts) return;
  if (opts.mode) setMode(opts.mode);
  if (opts.loMin !== undefined) form.loDur = opts.loMin;
  if (opts.hiMin !== undefined) form.hiDur = opts.hiMin;
  if (opts.injectLo !== undefined) form.loInj = opts.injectLo;
  if (opts.injectHi !== undefined) form.hiInj = opts.injectHi;
  if (opts.probeOnly !== undefined) form.probeOnly = !!opts.probeOnly;
}

onMounted(() => {
  applySaved(props.status.savedOptions);
  pickDefaultCourse();
});

// courses 由 App 侧异步拉取, 到货后补默认选中
watch(
  () => props.courses,
  (list) => {
    if (!courseId.value && list.length) pickDefaultCourse();
  }
);

function pickDefaultCourse() {
  applySaved(props.status.savedOptions);
  if (validCourses.value.length) {
    courseId.value = courseKey(validCourses.value[0]);
    loadCatalog(validCourses.value[0]);
  }
}

async function start() {
  const c = validCourses.value.find((x) => courseKey(x) === courseId.value);
  if (!c) { ElMessage.warning("先选一门课程"); return; }
  const payload = { mode: form.mode, schemeId: c.schemeId, classId: c.classId, offset: 0, limit: 0 };
  if (form.mode === "solve") {
    payload.probeOnly = form.probeOnly;
  } else {
    if (!(form.loDur > 0) || form.hiDur < form.loDur) {
      ElMessage.error("时长区间需要 0 以上，且下限不大于上限。");
      return;
    }
    payload.loMin = form.loDur;
    payload.hiMin = form.hiDur;
    payload.injectLo = form.loInj;
    payload.injectHi = form.hiInj;
  }
  if (qTotal.value > 0 && !(form.loLimit === 1 && form.hiLimit === qTotal.value)) {
    payload.offset = Math.max(0, form.loLimit - 1);
    payload.limit = Math.max(1, form.hiLimit - form.loLimit + 1);
  }
  starting.value = true;
  try {
    await api("/api/start", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    emit("started");
  } catch (err) {
    ElMessage.error(err.message);
  } finally {
    starting.value = false;
  }
}
</script>

<template>
  <el-card>
    <div class="card-head">
      <el-icon class="card-icon"><Reading /></el-icon>
      <h2>课程</h2>
      <el-button text circle :icon="'Refresh'" aria-label="刷新课程" @click="loadCatalog(validCourses.find((x) => courseKey(x) === courseId))" />
    </div>
    <el-select v-model="courseId" placeholder="选择课程" style="width:100%;" size="large" @change="onCourseChange">
      <el-option v-for="c in validCourses" :key="courseKey(c)" :value="courseKey(c)" :label="c.courseName + (c.termName ? ' · ' + c.termName : '')" />
    </el-select>
    <p v-if="!validCourses.length" class="card-sub" style="margin:12px 0 0;">这学期没有可刷的课程（实验课已排除）。</p>
    <p v-if="qHint" class="card-sub" style="margin:12px 0 0;">{{ qHint }}</p>
  </el-card>

  <el-card>
    <div class="card-head">
      <el-icon class="card-icon"><SwitchFilled /></el-icon>
      <h2>任务类型</h2>
    </div>
    <el-radio-group :model-value="form.mode" @update:model-value="setMode">
      <el-radio-button value="idle">挂机计时</el-radio-button>
      <el-radio-button value="solve">自动刷题</el-radio-button>
    </el-radio-group>
    <p class="card-sub" style="margin:12px 0 0;">
      {{ form.mode === "solve"
        ? "空桩探测判题回显拿答案表，交查表代码判过；实验课与已通过题自动跳过。"
        : "为未通过的题累积学习时长，已达标自动跳过，不动代码不判题。" }}
    </p>
  </el-card>

  <el-card v-if="form.mode === 'idle'">
    <div class="card-head">
      <el-icon class="card-icon"><Clock /></el-icon>
      <h2>每题时长</h2>
    </div>
    <div class="pair-row">
      <el-input-number v-model="form.loDur" :min="0.5" :max="180" :step="0.5" :precision="1" :controls="false" style="width:110px;" />
      <span class="pair-sep">–</span>
      <el-input-number v-model="form.hiDur" :min="0.5" :max="180" :step="0.5" :precision="1" :controls="false" style="width:110px;" />
      <span class="pair-unit">分钟 · 每题在区间内随机取目标，已达标跳过</span>
    </div>
    <div class="chip-row">
      <el-check-tag v-for="p in durationPresets" :key="p.label" :checked="form.loDur === p.lo && form.hiDur === p.hi" @change="form.loDur = p.lo; form.hiDur = p.hi">{{ p.label }}</el-check-tag>
    </div>
  </el-card>

  <el-card v-if="form.mode === 'idle'">
    <div class="card-head">
      <el-icon class="card-icon"><WarningFilled /></el-icon>
      <h2>错误注入</h2>
    </div>
    <div class="pair-row">
      <el-input-number v-model="form.loInj" :min="0" :max="50" :step="1" :precision="0" :controls="false" style="width:110px;" />
      <span class="pair-sep">–</span>
      <el-input-number v-model="form.hiInj" :min="0" :max="50" :step="1" :precision="0" :controls="false" style="width:110px;" />
      <span class="pair-unit">次 · 时长达标后模拟真实报错，存档快照还原</span>
    </div>
    <div class="chip-row">
      <el-check-tag v-for="p in injectPresets" :key="p.label" :checked="form.loInj === p.lo && form.hiInj === p.hi" @change="form.loInj = p.lo; form.hiInj = p.hi">{{ p.label }}</el-check-tag>
    </div>
  </el-card>

  <el-card v-if="form.mode === 'solve'">
    <div class="card-head">
      <el-icon class="card-icon"><MagicStick /></el-icon>
      <h2>刷题选项</h2>
    </div>
    <div class="pair-row">
      <el-switch v-model="form.probeOnly" />
      <span class="pair-unit">仅探测 · 只跑空桩拿答案表，不交查表代码，不改 pass</span>
    </div>
    <p class="card-sub" style="margin:12px 0 0;">空桩探测判题回显拿答案表，交查表代码判过；每题存档快照、判后原样还原。</p>
  </el-card>

  <el-card v-if="qTotal > 0">
    <div class="card-head">
      <el-icon class="card-icon"><Tickets /></el-icon>
      <h2>题号范围</h2>
    </div>
    <div class="pair-row">
      <el-input-number v-model="form.loLimit" :min="1" :max="qTotal" :step="1" :precision="0" :controls="false" style="width:110px;" />
      <span class="pair-sep">–</span>
      <el-input-number v-model="form.hiLimit" :min="1" :max="qTotal" :step="1" :precision="0" :controls="false" style="width:110px;" />
      <span class="pair-unit">题 · 按目录顺序</span>
    </div>
    <div class="chip-row">
      <el-check-tag :checked="form.loLimit === 1 && form.hiLimit === qTotal" @change="form.loLimit = 1; form.hiLimit = qTotal">全部题目</el-check-tag>
      <el-check-tag v-if="qTotal > 10" :checked="form.loLimit === 1 && form.hiLimit === Math.min(10, qTotal)" @change="form.loLimit = 1; form.hiLimit = Math.min(10, qTotal)">前 10 题</el-check-tag>
      <el-check-tag v-if="qTotal > 20" :checked="form.loLimit === 1 && form.hiLimit === Math.min(20, qTotal)" @change="form.loLimit = 1; form.hiLimit = Math.min(20, qTotal)">前 20 题</el-check-tag>
    </div>
  </el-card>

  <el-button type="primary" size="large" round :loading="starting" :disabled="!validCourses.length" @click="start">
    开始
  </el-button>
</template>

<style scoped>
.pair-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.pair-sep { color: var(--md-on-surface-variant); }
.pair-unit { font-size: 12px; color: var(--md-on-surface-variant); }
.chip-row { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 12px; }
</style>
