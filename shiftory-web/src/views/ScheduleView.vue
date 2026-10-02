<script setup lang="ts">
import PageHeader from "@/components/PageHeader.vue";
import EmptyWorkspace from "@/components/EmptyWorkspace.vue";
import ScheduleBoard from "@/components/ScheduleBoard.vue";
import { useSessionStore } from "@/stores/session";
const session = useSessionStore();
</script>
<template>
  <EmptyWorkspace v-if="!session.currentWorkspace" /><template v-else
    ><PageHeader
      eyebrow="MY SCHEDULE"
      title="我的排班"
      description="个人排班在所有工作区共享；当前工作区决定可用班次与操作权限。" /><ScheduleBoard
      :workspace-id="session.currentWorkspace.id"
      :user-id="session.user!.id"
  /></template>
</template>
