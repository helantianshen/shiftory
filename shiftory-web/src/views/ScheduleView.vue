<script setup lang="ts">
import PageHeader from "@/components/PageHeader.vue";
import ScheduleBoard from "@/components/ScheduleBoard.vue";
import { useSessionStore } from "@/stores/session";
const session = useSessionStore();
</script>
<template>
  <PageHeader
    eyebrow="MY SCHEDULE"
    title="我的排班"
    description="个人排班在所有工作区共享，查看和编辑无需选择工作区。"
  />
  <ScheduleBoard personal :user-id="session.user!.id" />
</template>
