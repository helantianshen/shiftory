<script setup lang="ts">
import { reactive, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import { CalendarCheck2, Eye, EyeOff, Sparkles } from "lucide-vue-next";
import { useSessionStore } from "@/stores/session";
const session = useSessionStore();
const router = useRouter();
const route = useRoute();
const mode = ref<"login" | "register">("login");
const showPassword = ref(false);
const loading = ref(false);
const form = reactive({
  login: "",
  username: "",
  email: "",
  displayName: "",
  password: "",
});
async function submit() {
  loading.value = true;
  try {
    if (mode.value === "login") await session.login(form.login, form.password);
    else
      await session.register({
        username: form.username,
        email: form.email,
        displayName: form.displayName,
        password: form.password,
      });
    ElMessage.success(mode.value === "login" ? "欢迎回来" : "账号创建成功");
    await router.replace(
      typeof route.query.redirect === "string"
        ? route.query.redirect
        : "/overview",
    );
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : "操作失败");
  } finally {
    loading.value = false;
  }
}
</script>
<template>
  <div class="auth-page">
    <section class="auth-visual">
      <div class="brand">
        <span class="brand-mark"><Sparkles /></span
        ><span>Shiftory<small>排班协同</small></span>
      </div>
      <h1>让每一次排班，都清晰可见。</h1>
      <p>
        导入截图与
        Excel，统一个人和团队排班，在同一张日历上找到工作、休息与共同空闲。
      </p>
      <div
        class="subtle-card"
        style="
          margin-top: 38px;
          background: rgba(255, 255, 255, 0.1);
          border-color: rgba(255, 255, 255, 0.18);
          color: #e6f4ef;
        "
      >
        <CalendarCheck2
          style="vertical-align: middle; margin-right: 8px"
        />工作、休息与缺失数据严格区分
      </div>
    </section>
    <section class="auth-panel">
      <div class="auth-card">
        <span class="eyebrow">WELCOME TO SHIFTORY</span>
        <h2>{{ mode === "login" ? "登录你的账号" : "创建新账号" }}</h2>
        <p>
          {{
            mode === "login"
              ? "继续管理你的个人与团队排班。"
              : "几分钟内建立你的第一个排班工作区。"
          }}
        </p>
        <el-form label-position="top" @submit.prevent="submit"
          ><template v-if="mode === 'register'"
            ><el-form-item label="用户名"
              ><el-input
                v-model="form.username"
                autocomplete="username" /></el-form-item
            ><el-form-item label="显示名称"
              ><el-input v-model="form.displayName" /></el-form-item
            ><el-form-item label="邮箱"
              ><el-input
                v-model="form.email"
                autocomplete="email" /></el-form-item></template
          ><el-form-item v-else label="用户名或邮箱"
            ><el-input
              v-model="form.login"
              autocomplete="username" /></el-form-item
          ><el-form-item label="密码"
            ><el-input
              v-model="form.password"
              :type="showPassword ? 'text' : 'password'"
              autocomplete="current-password"
              ><template #suffix
                ><button
                  type="button"
                  class="icon-button"
                  @click="showPassword = !showPassword"
                >
                  <EyeOff v-if="showPassword" :size="17" /><Eye
                    v-else
                    :size="17"
                  /></button></template></el-input></el-form-item
          ><el-button
            type="primary"
            native-type="submit"
            size="large"
            class="full-width"
            :loading="loading"
            >{{ mode === "login" ? "登录" : "注册并登录" }}</el-button
          ></el-form
        >
        <div class="auth-switch">
          {{ mode === "login" ? "还没有账号？" : "已经有账号？" }}
          <el-button
            text
            type="primary"
            @click="mode = mode === 'login' ? 'register' : 'login'"
            >{{ mode === "login" ? "立即注册" : "返回登录" }}</el-button
          >
        </div>
      </div>
    </section>
  </div>
</template>
