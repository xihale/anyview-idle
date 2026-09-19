<script setup>
import { ref, computed, onMounted, onUnmounted } from "vue";

const props = defineProps({
  status: { type: Object, default: () => ({}) },
  stopping: { type: Boolean, default: false },
});
const emit = defineEmits(["stop"]);

const now = ref(Date.now());
let timer;
onMounted(() => { timer = setInterval(() => { now.value = Date.now(); }, 1000); });
onUnmounted(() => clearInterval(timer));

const solve = computed(() => (props.status.mode || "idle") === "solve");
const cur = computed(() => props.status.current || null);
const min1 = (v) => (Math.round((Number(v) || 0) * 10) / 10).toFixed(1);

const hold = computed(() => {
  const c = cur.value;
  if (!c) return null;
  const elapsedMin = Math.max(0, (now.value - new Date(c.startedAt).getTime()) / 60000);
  const accumMin = Math.max(0, (c.accumSec || 0) / 60);
  const totalMin = accumMin + elapsedMin;
  const targetMin = Math.max(0.1, c.targetMin || 1);
  const span = Math.max(0.05, targetMin - accumMin);
  const pct = Math.min(100, Math.max(0, ((totalMin - accumMin) / span) * 100));
  return { totalMin, targetMin, accumMin, pct };
});

const total = computed(() => props.status.total || 0);
const processed = computed(() => (props.status.done || 0) + (props.status.skipped || 0) + (props.status.failed || 0));
const hasCur = computed(() => props.status.running && !!cur.value);
const overallPct = computed(() => (total.value > 0 ? Math.min(100, Math.round((processed.value / total.value) * 100)) : 0));
const remaining = computed(() => (total.value > 0 ? Math.max(0, total.value - processed.value - (hasCur.value ? 1 : 0)) : "—"));
const segPct = (n) => (total.value > 0 ? (n / total.value) * 100 : 0) + "%";
</script>

<template>
  <el-card>
    <div class="card-head">
      <el-icon class="card-icon"><Loading :class="{ spin: status.running }" /></el-icon>
      <h2>{{ cur ? cur.pname : (stopping ? "收尾中…" : "准备下一题…") }}</h2>
      <el-tag type="info" round effect="light">
        <span style="display:inline-flex;align-items:center;gap:6px;">
          <span class="chip-dot" />
          {{ solve ? "自动刷题" : "待机计时中" }}
        </span>
      </el-tag>
    </div>
    <p class="card-sub">
      {{ cur ? `${cur.chapName || "—"} · EID ${cur.eid || "—"}` : "换题间隙 · 防刷限速保护，随机等待 8~23s" }}
    </p>

    <template v-if="!solve">
      <div class="metric-big">
        <span class="num">{{ hold ? min1(hold.totalMin) : "···" }}</span>
        <span class="unit">/ {{ hold ? min1(hold.targetMin) : "—" }} 分钟</span>
      </div>
      <div class="bar-track">
        <div class="bar-fill" :style="{ width: (hold ? hold.pct : 0) + '%' }" />
      </div>
      <div class="bar-caption">
        <span v-if="hold">
          {{ hold.totalMin >= hold.targetMin ? "目标已达成，等待复核" : `还需约 ${min1(hold.targetMin - hold.totalMin)} 分钟` }}
        </span>
        <span v-else>等待中…</span>
        <span v-if="hold">{{ min1(hold.accumMin) }} → {{ min1(hold.totalMin) }} / {{ min1(hold.targetMin) }} 分钟</span>
      </div>
    </template>

    <template v-else>
      <div class="metric-big">
        <span class="num">{{ status.solved ?? 0 }}</span>
        <span class="unit">/ {{ total || "—" }} 题已通过</span>
      </div>
      <div class="bar-caption">
        <span>{{ cur ? (status.probeOnly ? "探测答案表中" : "生成查表代码并判题中") : "间隙等待" }}</span>
      </div>
    </template>

    <div class="substats">
      <div class="stat"><div class="lbl">完成</div><div class="val">{{ status.done ?? 0 }}</div></div>
      <div class="stat"><div class="lbl">跳过</div><div class="val">{{ status.skipped ?? 0 }}</div></div>
      <div class="stat"><div class="lbl">失败</div><div class="val">{{ status.failed ?? 0 }}</div></div>
      <div class="stat" v-if="solve"><div class="lbl">通过</div><div class="val">{{ status.solved ?? 0 }}</div></div>
      <div class="stat" v-if="!solve"><div class="lbl">剩余</div><div class="val">{{ remaining }}</div></div>
      <div class="stat" v-if="!solve"><div class="lbl">涨幅</div><div class="val">+{{ min1((status.gainedSec || 0) / 60) }} 分</div></div>
      <div class="stat" v-if="!solve"><div class="lbl">注入</div><div class="val">{{ status.injectedErrors ?? 0 }} 次</div></div>
    </div>

    <div class="bar-track" style="margin-top:14px;">
      <div class="bar-fill" :style="{ width: segPct(status.done || 0) }" />
      <div class="bar-fill muted" :style="{ width: segPct(status.skipped || 0) }" />
      <div class="bar-fill err" :style="{ width: segPct(status.failed || 0) }" />
    </div>
    <div class="bar-caption">
      <span>{{ total > 0 ? `第 ${Math.min(total, processed + (hasCur ? 1 : 0))} / ${total} 题` : `已处理 ${processed} 题` }}</span>
      <span v-if="total > 0">{{ overallPct }}%</span>
    </div>
  </el-card>

  <el-button type="warning" plain size="large" round @click="emit('stop')">停止</el-button>
  <p v-if="stopping" class="card-sub" style="text-align:center;margin:0;">当前题挂完即停并安全下线</p>
</template>

<style scoped>
.chip-dot { width: 8px; height: 8px; border-radius: 50%; background: currentColor; }
.spin { animation: rotate 1.2s linear infinite; }
@keyframes rotate { to { transform: rotate(360deg); } }
</style>
