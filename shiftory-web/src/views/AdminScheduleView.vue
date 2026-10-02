<script setup lang="ts">
import { computed, ref } from "vue";
import { useQuery } from "@tanstack/vue-query";
import PageHeader from "@/components/PageHeader.vue";
import ScheduleBoard from "@/components/ScheduleBoard.vue";
import { api } from "@/api/client";
import type { Member } from "@/api/types";
import { useSessionStore } from "@/stores/session";
const session = useSessionStore();
const workspaceID = computed(() => session.currentWorkspace?.id ?? 0);
const members = useQuery({
  enabled: computed(() => workspaceID.value > 0),
  queryKey: computed(() => ["members", workspaceID.value]),
  queryFn: () =>
    api.get<{ items: Member[] }>(`/workspaces/${workspaceID.value}/members`),
});
const selected = ref<number | undefined>();
</script>
<template>
  <PageHeader
    eyebrow="ADMINISTRATION"
    title="排班管理"
    description="管理当前工作区成员的排班；修改会同步到成员的其他工作区。"
    ><el-select v-model="selected" placeholder="选择成员" filterable
      ><el-option
        v-for="member in members.data.value?.items.filter(
          (i) => i.status === 'ACTIVE',
        ) ?? []"
        :key="member.id"
        :label="member.displayName"
        :value="member.id" /></el-select
  ></PageHeader>
  <div v-if="!selected" class="surface-card card-padding subtle-card">
    请先选择要管理的成员。
  </div>
  <ScheduleBoard v-else :workspace-id="workspaceID" :user-id="selected" />
</template>
