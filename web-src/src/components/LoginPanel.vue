<script setup>
import { reactive, ref } from "vue";
import { ElMessage } from "element-plus";
import { api } from "../api";

const emit = defineEmits(["logged-in"]);

const form = reactive({
  username: "",
  password: "",
  token: "",
  xff: localStorage.getItem("anyview_xff") || "",
});
const loading = ref(false);
const advOpen = ref(false);
const formRef = ref();

const rules = {
  username: [{ required: true, message: "填学号, 或直接用 Token", trigger: "blur" }],
  password: [{ required: true, message: "填密码, 或直接用 Token", trigger: "blur" }],
};

async function submit() {
  const token = form.token.trim();
  if (!token) {
    try { await formRef.value.validate(); } catch { return; }
  }
  loading.value = true;
  try {
    const body = await api("/api/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        username: token ? "" : form.username.trim(),
        password: token ? "" : form.password,
        token,
        xff: form.xff.trim(),
      }),
    });
    if (form.xff.trim()) localStorage.setItem("anyview_xff", form.xff.trim());
    const name = (body.student && (body.student.name || body.student.no)) || "在线";
    ElMessage.success(`已登录: ${name}`);
    emit("logged-in", name);
  } catch (err) {
    ElMessage.error(err.message + "。检查学号密码, 或稍后再试。");
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <el-card>
    <div class="card-head">
      <h2>登录</h2>
    </div>
    <p class="card-sub">安静累积课程时长与做题痕迹 · 任务结束自动下线让位</p>

    <el-form ref="formRef" :model="form" :rules="rules" label-position="top" @submit.prevent="submit">
      <el-form-item label="学号" prop="username">
        <el-input
          v-model="form.username"
          name="username"
          :maxlength="32"
          placeholder="输入学号…"
          autocomplete="username"
          :spellcheck="false"
        />
      </el-form-item>
      <el-form-item label="密码" prop="password">
        <el-input
          v-model="form.password"
          name="password"
          type="password"
          show-password
          placeholder="输入密码…"
          autocomplete="current-password"
        />
      </el-form-item>

      <el-button text size="small" class="adv-toggle" @click="advOpen = !advOpen">
        高级选项
        <el-icon class="el-icon--right"><ArrowDown /></el-icon>
      </el-button>
      <el-collapse-transition>
        <div v-show="advOpen" class="adv-body">
          <el-form-item label="Token">
            <el-input
              v-model="form.token"
              name="token"
              placeholder="已有 Token 可跳过账号密码, 约 12h 有效"
              autocomplete="off"
              :spellcheck="false"
            />
          </el-form-item>
          <el-form-item label="X-Forwarded-For">
            <el-input
              v-model="form.xff"
              name="xff"
              placeholder="直连源站时填写公网出口 IP"
              autocomplete="off"
              :spellcheck="false"
            />
          </el-form-item>
        </div>
      </el-collapse-transition>

      <el-button type="primary" size="large" round class="submit" native-type="submit" :loading="loading">
        登录
      </el-button>
    </el-form>
  </el-card>
</template>

<style scoped>
.adv-toggle { margin-bottom: 4px; }
.adv-body { margin-bottom: 4px; }
.submit { width: 100%; margin-top: 16px; }
</style>
