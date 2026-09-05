<script setup lang="ts">
import { reactive, ref } from "vue";
import { ElMessage } from "element-plus";
import { api } from "@/api/client";
import PageHeader from "@/components/PageHeader.vue";
import { useSessionStore, type User } from "@/stores/session";
const session = useSessionStore();
const profile = reactive({
  displayName: session.user?.displayName ?? "",
  avatarUrl: session.user?.avatarUrl ?? "",
});
const password = reactive({ currentPassword: "", newPassword: "" });
const saving = ref(false);
async function saveProfile() {
  saving.value = true;
  try {
    const user = await api.patch<User>("/auth/profile", profile);
    if (session.accessToken) session.setSession(user, session.accessToken);
    ElMessage.success("个人资料已更新");
  } finally {
    saving.value = false;
  }
}
async function changePassword() {
  await api.put("/auth/password", password);
  password.currentPassword = "";
  password.newPassword = "";
  session.setAccessToken(null);
  ElMessage.success("密码已修改，请重新登录");
}
</script>
<template>
  <PageHeader
    eyebrow="PROFILE"
    title="个人资料"
    description="个人信息保存在账号中，并由 Pinia 维护当前会话展示。"
  />
  <div class="grid-2">
    <section class="surface-card card-padding">
      <h2 class="section-title">公开资料</h2>
      <el-form label-position="top" @submit.prevent="saveProfile"
        ><el-form-item label="显示名称"
          ><el-input v-model="profile.displayName" /></el-form-item
        ><el-form-item label="头像 URL"
          ><el-input
            v-model="profile.avatarUrl"
            placeholder="https://…" /></el-form-item
        ><el-button type="primary" native-type="submit" :loading="saving"
          >保存资料</el-button
        ></el-form
      >
    </section>
    <section class="surface-card card-padding">
      <h2 class="section-title">修改密码</h2>
      <p class="muted">修改后会撤销当前账号的全部 Refresh Token。</p>
      <el-form label-position="top" @submit.prevent="changePassword"
        ><el-form-item label="当前密码"
          ><el-input
            v-model="password.currentPassword"
            type="password"
            show-password /></el-form-item
        ><el-form-item label="新密码"
          ><el-input
            v-model="password.newPassword"
            type="password"
            show-password /></el-form-item
        ><el-button native-type="submit">更新密码</el-button></el-form
      >
    </section>
  </div>
</template>
