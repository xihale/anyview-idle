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
  { lo: 3, hi: 6 },
  { lo: 5, hi: 10 },
  { lo: 10, hi: 20 },
  { lo: 15, hi: 30 },
];
const injectPresets = [
  { lo: 0, hi: 0, label: "仅时长" },
  { lo: 1, hi: 3 },
  { lo: 3, hi: 6 },
];

const modeDesc = computed(() =>
  form.mode === "solve"
    ? "空桩探测从判题回显取答案表, 生成查表代码提交判过; 存档快照逐题还原, 已通过与实验课自动跳过。"
    : "给未通过的题累积学习时长; 每题在区间内随机取目标, 达标自动跳过, 不动代码不判题。"
);

function setMode(m) {
  form.mode = m;
  localStorage.setItem("anv-mode", m);
}

function selectedCourse() {
  return validCourses.value.find((x) => courseKey(x) === courseId.value);
}

function onCourseChange(key) {
  const c = validCourses.value.find((x) => courseKey(x) === key);
  if (!c) return;
  loadCatalog(c);
}

function reloadCatalog() {
  const c = selectedCourse();
  if (c) loadCatalog(c);
}

async function loadCatalog(c) {
  qHint.value = "读取目录…";
  try {
    const data = await api(`/api/catalog?schemeId=${c.schemeId}&classId=${c.classId}`);
    qTotal.value = Math.max(1, data.total || 1);
    form.hiLimit = qTotal.value;
    qHint.value = `共 ${qTotal.value} 题` + (data.unpassed !== undefined ? ` · ${data.unpassed} 题待刷` : "");
  } catch {
    qTotal.value = 0;
    qHint.value = "";
    ElMessage.warning("目录暂时读不到, 将按全部未通过题开刷");
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
  const c = selectedCourse();
  if (!c) { ElMessage.warning("先选一门课程"); return; }
  const payload = { mode: form.mode, schemeId: c.schemeId, classId: c.classId, offset: 0, limit: 0 };
  if (form.mode === "solve") {
    payload.probeOnly = form.probeOnly;
  } else {
    if (!(form.loDur > 0) || form.hiDur < form.loDur) {
      ElMessage.error("时长区间需要 0 以上, 且下限不大于上限");
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
      <h2>新任务</h2>
    </div>

    <el-form label-position="top" @submit.prevent="start">
      <el-form-item label="课程">
        <div class="course-row">
          <el-select
            v-model="courseId"
            filterable
            placeholder="选择课程"
            class="course-select"
            size="large"
            @change="onCourseChange"
          >
            <el-option
              v-for="c in validCourses"
              :key="courseKey(c)"
              :value="courseKey(c)"
              :label="c.courseName + (c.termName ? ' (' + c.termName + ')' : '')"
            />
          </el-select>
          <el-tooltip content="重新读取目录" placement="top">
            <el-button text circle icon="Refresh" aria-label="刷新课程目录" @click="reloadCatalog" />
          </el-tooltip>
        </div>
        <div v-if="!validCourses.length" class="form-hint">这学期没有可刷的课程 (实验课已排除)。</div>
        <div v-else-if="qHint" class="form-hint">{{ qHint }}</div>
      </el-form-item>

      <el-form-item label="任务类型">
        <el-radio-group :model-value="form.mode" @update:model-value="setMode">
          <el-radio-button value="idle">挂机计时</el-radio-button>
          <el-radio-button value="solve">自动刷题</el-radio-button>
        </el-radio-group>
        <div class="form-hint">{{ modeDesc }}</div>
      </el-form-item>

      <template v-if="form.mode === 'idle'">
        <el-form-item label="每题时长 (分钟)">
          <div class="pair-row">
            <el-input-number v-model="form.loDur" :min="0.5" :max="180" :step="0.5" :precision="1" :controls="false" class="pair-num" />
            <span class="pair-sep">-</span>
            <el-input-number v-model="form.hiDur" :min="0.5" :max="180" :step="0.5" :precision="1" :controls="false" class="pair-num" />
            <el-check-tag
              v-for="p in durationPresets"
              :key="p.lo + '-' + p.hi"
              :checked="form.loDur === p.lo && form.hiDur === p.hi"
              @change="form.loDur = p.lo; form.hiDur = p.hi"
            >{{ p.lo }}-{{ p.hi }}</el-check-tag>
          </div>
          <div class="form-hint">每题在区间内随机取目标, 累计到达后跳过</div>
        </el-form-item>

        <el-form-item label="错误注入 (次)">
          <div class="pair-row">
            <el-input-number v-model="form.loInj" :min="0" :max="50" :step="1" :precision="0" :controls="false" class="pair-num" />
            <span class="pair-sep">-</span>
            <el-input-number v-model="form.hiInj" :min="0" :max="50" :step="1" :precision="0" :controls="false" class="pair-num" />
            <el-check-tag
              v-for="p in injectPresets"
              :key="p.label || p.lo + '-' + p.hi"
              :checked="form.loInj === p.lo && form.hiInj === p.hi"
              @change="form.loInj = p.lo; form.hiInj = p.hi"
            >{{ p.label || `${p.lo}-${p.hi}` }}</el-check-tag>
          </div>
          <div class="form-hint">时长达标后模拟真实报错, 存档快照还原</div>
        </el-form-item>
      </template>

      <el-form-item v-else label="选项">
        <el-switch v-model="form.probeOnly" />
        <span class="form-hint inline">仅探测: 只跑空桩拿答案表, 不交查表代码, 不改 pass</span>
      </el-form-item>

      <el-form-item v-if="qTotal > 0" label="题号范围">
        <div class="pair-row">
          <el-input-number v-model="form.loLimit" :min="1" :max="qTotal" :step="1" :precision="0" :controls="false" class="pair-num" />
          <span class="pair-sep">-</span>
          <el-input-number v-model="form.hiLimit" :min="1" :max="qTotal" :step="1" :precision="0" :controls="false" class="pair-num" />
          <el-check-tag :checked="form.loLimit === 1 && form.hiLimit === qTotal" @change="form.loLimit = 1; form.hiLimit = qTotal">全部</el-check-tag>
          <el-check-tag
            v-if="qTotal > 10"
            :checked="form.loLimit === 1 && form.hiLimit === Math.min(10, qTotal)"
            @change="form.loLimit = 1; form.hiLimit = Math.min(10, qTotal)"
          >前 10</el-check-tag>
          <el-check-tag
            v-if="qTotal > 20"
            :checked="form.loLimit === 1 && form.hiLimit === Math.min(20, qTotal)"
            @change="form.loLimit = 1; form.hiLimit = Math.min(20, qTotal)"
          >前 20</el-check-tag>
        </div>
        <div class="form-hint">按目录顺序执行</div>
      </el-form-item>

      <div class="card-foot">
        <el-button type="primary" size="large" round native-type="submit" :loading="starting" :disabled="!validCourses.length">
          开始
        </el-button>
      </div>
    </el-form>
  </el-card>
</template>

<style scoped>
.course-row { display: flex; align-items: center; gap: 8px; width: 100%; }
.course-select { flex: 1; }
.pair-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; width: 100%; }
.pair-num { width: 96px; }
.pair-sep { color: var(--md-on-surface-variant); }
.form-hint { font-size: 12px; color: var(--md-on-surface-variant); margin-top: 6px; width: 100%; }
.form-hint.inline { margin: 0 0 0 10px; width: auto; }
</style>
