<script setup lang="ts">
import { ref } from "vue";
import { ElMessage } from "element-plus";
import { Building2 } from "lucide-vue-next";
import { api } from "@/api/client";
import { useSessionStore, type Workspace } from "@/stores/session";
const session = useSessionStore();
const name = ref("我的排班空间");
const timezone = ref("Asia/Shanghai");
const saving = ref(false);
async function createWorkspace() {
  saving.value = true;
  try {
    const workspace = await api.post<Workspace>("/workspaces", {
      name: name.value,
      timezone: timezone.value,
    });
    session.setWorkspaces([...session.workspaces, workspace]);
    await session.selectWorkspace(workspace.id);
    ElMessage.success("工作区已创建");
  } finally {
    saving.value = false;
  }
}
</script>
<template>
  <section class="empty-workspace surface-card">
    <div class="empty-icon"><Building2 /></div>
    <h2>创建第一个工作区</h2>
    <p>工作区用于隔离成员、班次和排班数据。你稍后还可以加入更多工作区。</p>
    <el-form label-position="top" @submit.prevent="createWorkspace"
      ><el-form-item label="工作区名称"
        ><el-input v-model="name" maxlength="80" /></el-form-item
      ><el-form-item label="时区"
        ><el-select v-model="timezone"
          ><el-option
            label="中国标准时间 · Asia/Shanghai"
            value="Asia/Shanghai" /></el-select></el-form-item
      ><el-button type="primary" native-type="submit" :loading="saving"
        >创建并进入</el-button
      ></el-form
    >
  </section>
</template>
