<script setup>
import { ref, computed, watch, nextTick } from "vue";
import { ElMessage } from "element-plus";

const props = defineProps({
  status: { type: Object, default: () => ({}) },
});

const autoScroll = ref(true);
const filter = ref("all");
const scrollBox = ref(null);

// 后端日志行 = "HH:MM:SS  正文"; 行首的 ▶ ■ ☞ ✓ ✗ ⚠ 是级别信号,
// 解析成 timeline 节点颜色后从正文里剥掉, 由组件自己表达。
const GLYPH = /^\s*(?:▶|■|☞|✓|✗|⚠|↻)\s*/;

const entries = computed(() =>
  (props.status?.log || []).map((line) => {
    const raw = String(line);
    const m = raw.match(/^(\d{1,2}:\d{2}:\d{2})\s*(.*)$/);
    const probe = m ? m[2] : raw;
    const text = probe.replace(GLYPH, "");
    // 级别在剥除信号符之前判; ■ 收尾总结行 (刷题完毕/处理完毕) 是正常终结事件, 不因带"失败 N"而标红
    let level = "info";
    if (/■/.test(probe) && /完毕|完成|结束/.test(probe) && !/中止/.test(probe)) level = "success";
    else if (/失败|错误|中止|异常|未过|⚠|✗/.test(probe)) level = "danger";
    else if (/✓|通过|完成|已落/.test(probe)) level = "success";
    return { time: m ? m[1] : "", text, level };
  })
);

const errCount = computed(() => entries.value.filter((e) => e.level === "danger").length);
const shown = computed(() =>
  filter.value === "warn" ? entries.value.filter((e) => e.level === "danger") : entries.value.slice(-400)
);

const filterOptions = computed(() => [
  { label: "全部", value: "all" },
  { label: errCount.value ? `异常 ${errCount.value}` : "异常", value: "warn" },
]);

watch(
  () => [entries.value.length, filter.value],
  () => {
    if (!autoScroll.value) return;
    nextTick(() => {
      const box = scrollBox.value;
      if (box) box.scrollTop = box.scrollHeight;
    });
  },
  { immediate: true }
);

async function copyAll() {
  try {
    await navigator.clipboard.writeText((props.status?.log || []).join("\n"));
    ElMessage.success("已复制全文");
  } catch {
    ElMessage.error("复制失败");
  }
}
</script>

<template>
  <el-card v-if="entries.length">
    <div class="card-head">
      <h2>运行日志</h2>
      <el-segmented v-model="filter" :options="filterOptions" size="small" />
      <el-tooltip content="有新日志时自动滚到底部" placement="top">
        <el-switch v-model="autoScroll" size="small" />
      </el-tooltip>
      <el-button text size="small" @click="copyAll">复制全部</el-button>
    </div>

    <div ref="scrollBox" class="log-scroll">
      <el-timeline>
        <el-timeline-item
          v-for="(e, i) in shown"
          :key="entries.length - shown.length + i"
          :type="e.level === 'info' ? '' : e.level"
          :hollow="e.level === 'info'"
          hide-timestamp
        >
          <span v-if="e.time" class="log-time tnum">{{ e.time }}</span>{{ e.text }}
        </el-timeline-item>
      </el-timeline>
    </div>
  </el-card>
</template>
