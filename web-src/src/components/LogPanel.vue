<script setup>
import { ref, watch } from "vue";
import { ElMessage } from "element-plus";

const props = defineProps({
  status: { type: Object, default: () => ({}) },
});

const autoScroll = ref(true);
const body = ref(null);
const lines = ref([]);

watch(
  () => (props.status ? props.status.log : null),
  (log) => {
    lines.value = log || [];
    if (autoScroll.value) {
      requestAnimationFrame(() => {
        if (body.value) body.value.scrollTop = body.value.scrollHeight;
      });
    }
  },
  { immediate: true }
);

async function copyAll() {
  try {
    await navigator.clipboard.writeText(lines.value.join("\n"));
    ElMessage.success("已复制全文");
  } catch {
    ElMessage.error("复制失败");
  }
}
</script>

<template>
  <el-card v-if="lines.length">
    <div class="card-head">
      <el-icon class="card-icon"><Document /></el-icon>
      <h2>运行日志 · {{ lines.length }} 行</h2>
      <el-switch v-model="autoScroll" size="small" inline-prompt active-text="滚屏" inactive-text="滚屏" style="margin-right:8px;" />
      <el-button text size="small" @click="copyAll">复制全文</el-button>
    </div>
    <pre class="log-body" ref="body">{{ lines.slice(-200).join("\n") }}</pre>
  </el-card>
</template>
