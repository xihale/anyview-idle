<script setup>
import { computed } from "vue";
import { api, SHOT } from "../api";

const props = defineProps({
  status: { type: Object, default: () => ({}) },
});
const emit = defineEmits(["back"]);

const solve = computed(() => (props.status.mode || "idle") === "solve");
const modeLabel = computed(() => (solve.value ? "自动刷题" : "挂机计时"));
const min1 = (v) => (Math.round((Number(v) || 0) * 10) / 10).toFixed(1);

const note = computed(() => {
  const st = props.status;
  if (st.error) return "这一轮中止。返回配置后可再开。";
  if (!st.authenticated && st.resumeAt) {
    const wait = new Date(st.resumeAt).getTime() - Date.now();
    const m = Math.max(1, Math.round(wait / 60000));
    const delay = m >= 60 ? `${Math.floor(m / 60)} 小时 ${m % 60} 分` : `${m} 分钟`;
    return `本轮中止, 已下线让位; 约 ${delay}后自动重登续跑。`;
  }
  if (!st.authenticated) return "本轮已结束, 账号已自动下线。再开一轮请重新登录。";
  return solve.value
    ? "已通过题的存档已原样还原, pass 记录在服务端账上。"
    : "任务已顺利完成, 凭据已安全清退。";
});

async function back() {
  if (!SHOT) { try { await api("/api/status"); } catch {} }
  emit("back");
}
</script>

<template>
  <el-card>
    <el-result
      :icon="status.error ? 'warning' : 'success'"
      :title="status.error ? '本轮中止' : '本轮完成'"
    >
      <template #sub-title>
        <p class="done-note">{{ modeLabel }} · {{ note }}</p>
        <div class="done-stats">
          <el-statistic class="stat-hero" :precision="solve ? 0 : 1" :value="solve ? (status.solved ?? 0) : (status.gainedSec || 0) / 60">
            <template #suffix>
              <span class="hero-suffix">{{ solve ? "题通过" : "分钟" }}</span>
            </template>
          </el-statistic>
          <div class="stat-row">
            <el-statistic class="stat-sm" :title="solve ? '探测完成' : '挂机达标'" :value="status.done ?? 0" />
            <el-statistic class="stat-sm" title="达标跳过" :value="status.skipped ?? 0" />
            <el-statistic class="stat-sm" title="异常失败" :value="status.failed ?? 0" />
            <el-statistic v-if="solve" class="stat-sm" title="判题通过" :value="status.solved ?? 0" />
            <el-statistic v-if="!solve" class="stat-sm" title="错误注入" :value="status.injectedErrors ?? 0" />
            <el-statistic v-if="!solve" class="stat-sm" title="累计涨幅 (分)" :value="(status.gainedSec || 0) / 60" :precision="1" />
          </div>
        </div>
      </template>

      <template #extra>
        <el-button type="primary" round @click="back">返回配置</el-button>
        <el-button v-if="!status.authenticated" tag="a" href="/" round>重新登录</el-button>
      </template>
    </el-result>
  </el-card>
</template>

<style scoped>
.done-note { margin: 0; }
.done-stats {
  display: flex; flex-direction: column; align-items: center; gap: 4px;
  margin-top: 20px;
}
.done-stats .stat-row { justify-content: center; }
.hero-suffix {
  font-size: 16px;
  color: var(--md-on-surface-variant);
  margin-left: 6px;
  font-weight: 400;
}
</style>
