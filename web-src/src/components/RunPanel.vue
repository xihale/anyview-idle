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

// 当前题: 现累计 + 本次实挂 → 距目标点的进度
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
const remaining = computed(() => Math.max(0, total.value - processed.value - (hasCur.value ? 1 : 0)));
const headTitle = computed(() => (cur.value ? cur.value.pname : stopping.value ? "收尾中…" : "准备下一题…"));
</script>

<template>
  <el-card>
    <div class="card-head">
      <h2>{{ headTitle }}</h2>
      <el-tag :type="stopping ? 'warning' : 'success'" effect="light" round>
        {{ solve ? "自动刷题" : "待机计时" }}
      </el-tag>
    </div>
    <p class="card-sub">
      {{ cur ? `${cur.chapName || ""} EID ${cur.eid || ""}`.trim() : "换题间隙 · 防刷限速保护, 随机等待 8~23s" }}
    </p>

    <template v-if="!solve">
      <el-statistic class="stat-hero" :value="hold ? hold.totalMin : 0" :precision="1">
        <template #suffix>
          <span class="hero-suffix">/ {{ hold ? min1(hold.targetMin) : "-" }} 分钟</span>
        </template>
      </el-statistic>
      <el-progress
        :percentage="hold ? Math.round(hold.pct) : 0"
        :stroke-width="8"
        :show-text="false"
        :status="hold && hold.pct >= 100 ? 'success' : ''"
      />
      <p class="card-sub">
        <template v-if="hold">
          {{ hold.pct >= 100 ? "目标已达成, 等待复核" : `还需约 ${min1(hold.targetMin - hold.totalMin)} 分钟` }}
          <span class="tnum"> · 累计 {{ min1(hold.accumMin) }} → {{ min1(hold.totalMin) }}</span>
        </template>
        <template v-else>等待中…</template>
      </p>
    </template>

    <template v-else>
      <el-statistic class="stat-hero" :value="status.solved ?? 0" :precision="0">
        <template #suffix>
          <span class="hero-suffix">/ {{ total || "-" }} 题已通过</span>
        </template>
      </el-statistic>
      <p class="card-sub">
        {{ cur ? (status.probeOnly ? "探测答案表中" : "生成查表代码并判题中") : "间隙等待" }}
      </p>
    </template>

    <div class="stat-row">
      <el-statistic class="stat-sm" title="完成" :value="status.done ?? 0" />
      <el-statistic class="stat-sm" title="跳过" :value="status.skipped ?? 0" />
      <el-statistic class="stat-sm" title="失败" :value="status.failed ?? 0" />
      <el-statistic v-if="solve" class="stat-sm" title="通过" :value="status.solved ?? 0" />
      <el-statistic v-if="!solve && total > 0" class="stat-sm" title="剩余" :value="remaining" />
      <el-statistic v-if="!solve" class="stat-sm" title="涨幅 (分)" :value="(status.gainedSec || 0) / 60" :precision="1" />
      <el-statistic v-if="!solve" class="stat-sm" title="注入" :value="status.injectedErrors ?? 0" />
    </div>

    <div class="overall">
      <el-progress
        :percentage="overallPct"
        :stroke-width="8"
        :show-text="false"
      />
      <p class="card-sub overall-cap">
        <span class="tnum">{{ total > 0 ? `第 ${Math.min(total, processed + (hasCur ? 1 : 0))} / ${total} 题` : `已处理 ${processed} 题` }}</span>
        <span v-if="total > 0" class="tnum">{{ overallPct }}%</span>
      </p>
    </div>

    <div class="card-foot">
      <el-tooltip content="当前题挂完即停, 随后安全下线" placement="top">
        <el-button type="danger" plain round :loading="stopping" @click="emit('stop')">停止</el-button>
      </el-tooltip>
    </div>
  </el-card>
</template>

<style scoped>
.hero-suffix {
  font-size: 16px;
  color: var(--md-on-surface-variant);
  margin-left: 6px;
  font-weight: 400;
}
.overall { margin-top: 18px; }
.overall-cap { display: flex; justify-content: space-between; margin-top: 6px; }
</style>
