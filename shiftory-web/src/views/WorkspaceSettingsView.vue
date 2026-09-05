<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { useRouter } from "vue-router";
import { ElMessage, ElMessageBox } from "element-plus";
import { api } from "@/api/client";
import type { Member } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import { useSessionStore } from "@/stores/session";
const session = useSessionStore();
const router = useRouter();
const workspace = computed(() => session.currentWorkspace!);
const form = reactive({ name: "", timezone: "Asia/Shanghai" });
watch(
  workspace,
  (value) =>
    Object.assign(form, { name: value.name, timezone: value.timezone }),
  { immediate: true },
);
const members = useQuery({
  queryKey: computed(() => ["members", workspace.value.id]),
  queryFn: () =>
    api.get<{ items: Member[] }>(`/workspaces/${workspace.value.id}/members`),
});
const newOwner = ref<number>();
const confirmation = ref("");
async function save() {
  await api.patch(`/workspaces/${workspace.value.id}`, form);
  session.setWorkspaces(
    session.workspaces.map((item) =>
      item.id === workspace.value.id ? { ...item, ...form } : item,
    ),
  );
  ElMessage.success("工作区设置已保存");
}
async function transfer() {
  if (!newOwner.value) return;
  await ElMessageBox.confirm(
    "转让后你将变为管理员，新所有者拥有解散工作区等最高权限。",
    "转让工作区",
    { type: "warning" },
  );
  await api.post(`/workspaces/${workspace.value.id}/transfer`, {
    newOwnerUserId: newOwner.value,
  });
  await session.loadContext();
  ElMessage.success("所有权已转让");
}
async function remove() {
  await ElMessageBox.confirm(
    "此操作会永久删除工作区内排班、导入与成员关系，无法撤销。",
    "最终确认",
    { type: "error" },
  );
  await api.delete(`/workspaces/${workspace.value.id}`, {
    confirmationName: confirmation.value,
  });
  await session.loadContext();
  ElMessage.success("工作区已解散");
  await router.push("/overview");
}
</script>
<template>
  <PageHeader
    eyebrow="WORKSPACE"
    title="工作区设置"
    description="时区决定排班日期解释；审计与任务租约始终按 UTC 保存。"
  />
  <div class="grid-2">
    <section class="surface-card card-padding">
      <h2 class="section-title">基础信息</h2>
      <el-form label-position="top" @submit.prevent="save"
        ><el-form-item label="工作区名称"
          ><el-input v-model="form.name" /></el-form-item
        ><el-form-item label="IANA 时区"
          ><el-select v-model="form.timezone"
            ><el-option label="Asia/Shanghai" value="Asia/Shanghai" /><el-option
              label="Asia/Tokyo"
              value="Asia/Tokyo" /><el-option
              label="UTC"
              value="UTC" /></el-select></el-form-item
        ><el-button type="primary" native-type="submit"
          >保存设置</el-button
        ></el-form
      >
    </section>
    <section v-if="session.isOwner" class="surface-card card-padding">
      <h2 class="section-title">转让所有权</h2>
      <p class="muted">只能转让给当前有效成员。</p>
      <el-select v-model="newOwner" placeholder="选择新所有者"
        ><el-option
          v-for="member in members.data.value?.items.filter(
            (i) => i.status === 'ACTIVE' && i.id !== session.user?.id,
          ) ?? []"
          :key="member.id"
          :label="member.displayName"
          :value="member.id" /></el-select
      ><el-button style="margin-left: 8px" @click="transfer">转让</el-button>
    </section>
  </div>
  <section
    v-if="session.isOwner"
    class="surface-card card-padding"
    style="margin-top: 18px; border-color: #f0c7cb"
  >
    <h2 class="section-title" style="color: var(--danger)">危险区域</h2>
    <p class="muted">
      输入完整工作区名称“{{
        workspace.name
      }}”后才能解散。原始导入文件也会一并删除。
    </p>
    <el-input
      v-model="confirmation"
      style="max-width: 360px; margin-right: 8px"
    /><el-button
      type="danger"
      :disabled="confirmation !== workspace.name"
      @click="remove"
      >解散工作区</el-button
    >
  </section>
</template>
