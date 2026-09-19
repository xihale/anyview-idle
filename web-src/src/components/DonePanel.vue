<script setup>
import { computed } from "vue";
import { api, SHOT } from "../api";

const props = defineProps({
  status: { type: Object, default: () => ({}) },
});
const emit = defineEmits(["back"]);

const solve = computed(() => (props.status.mode || "idle") === "solve");
const min1 = (v) => (Math.round((Number(v) || 0) * 10) / 10).toFixed(1);

const note = computed(() => {
  const st = props.status;
  if (st.error) return "这一轮中止。返回配置后可再开。";
  if (!st.authenticated && st.resumeAt) {
    const wait = new Date(st.resumeAt).getTime() - Date.now();
    const m = Math.max(1, Math.round(wait / 60000));
    const delay = m >= 60 ? `${Math.floor(m / 60)} 小时 ${m % 60} 分` : `${m} 分钟`;
    return "本轮中止，已下线让位；约 " + delay + "后自动重登续跑。";
  }
  if (!st.authenticated) return "本轮已结束，账号已自动下线。再开一轮请重新登录。";
  return solve.value
    ? "已通过题的存档已原样还原，pass 记录在服务端账上。"
    : "任务已顺利完成，凭据已安全清退。";
});

async function back() {
  if (!SHOT) { try { await api("/api/status"); } catch {} }
  emit("back");
}
</script>

<template>
  <el-card>
    <div class="card-head">
      <el-icon class="card-icon"><CircleCheck /></el-icon>
      <h2>本轮完成</h2>
      <el-tag type="info" round effect="light">{{ solve ? "自动刷题" : "挂机计时" }}</el-tag>
    </div>

    <div class="metric-big" v-if="solve">
      <span class="num">{{ status.solved ?? 0 }}</span>
      <span class="unit">题通过</span>
    </div>
    <div class="metric-big" v-else>
      <span class="num">{{ min1((status.gainedSec || 0) / 60) }}</span>
      <span class="unit">分钟</span>
    </div>

    <el-descriptions :column="2" border>
      <el-descriptions-item :label="solve ? '探测完成' : '挂机达标'">{{ status.done ?? 0 }}</el-descriptions-item>
      <el-descriptions-item label="达标跳过">{{ status.skipped ?? 0 }}</el-descriptions-item>
      <el-descriptions-item label="异常失败">{{ status.failed ?? 0 }}</el-descriptions-item>
      <el-descriptions-item v-if="solve" label="判题通过">{{ status.solved ?? 0 }}</el-descriptions-item>
      <el-descriptions-item v-if="!solve" label="错误注入">{{ status.injectedErrors ?? 0 }} 次</el-descriptions-item>
      <el-descriptions-item v-if="!solve" label="累计涨幅">+{{ min1((status.gainedSec || 0) / 60) }} 分</el-descriptions-item>
    </el-descriptions>

    <p class="card-sub" style="margin:16px 0;">{{ note }}</p>
    <el-button type="primary" round @click="back">返回配置</el-button>
    <el-button v-if="!status.authenticated" tag="a" href="/" round>重新登录</el-button>
  </el-card>
</template>
