<script setup lang="ts">
import { computed } from "vue";
import { RouterLink } from "vue-router";
import {
  CalendarDays,
  Clock3,
  FileClock,
  Import,
  LayoutDashboard,
  Settings,
  Users,
  UserRound,
  WandSparkles,
} from "lucide-vue-next";
import { useSessionStore } from "@/stores/session";

const session = useSessionStore();
const personal = [
  { to: "/schedule", label: "我的排班", icon: Clock3 },
  { to: "/profile", label: "个人资料", icon: UserRound },
];
const workspace = [
  { to: "/import", label: "导入排班", icon: Import },
  { to: "/calendar", label: "团队日历", icon: CalendarDays },
  { to: "/imports", label: "我的导入记录", icon: FileClock },
];
const administration = [
  { to: "/admin/schedules", label: "排班管理", icon: WandSparkles },
  { to: "/admin/members", label: "成员管理", icon: Users },
  { to: "/admin/shifts", label: "班次设置", icon: Clock3 },
  { to: "/admin/imports", label: "全部导入记录", icon: FileClock },
  { to: "/admin/workspace", label: "工作区设置", icon: Settings },
];
const groups = computed(() => [
  { label: "个人", items: personal },
  { label: "工作区", items: session.isAdmin ? [...workspace, ...administration] : workspace },
]);
</script>
<template>
  <nav class="navigation-menu" aria-label="主导航">
    <RouterLink to="/overview" class="navigation-item">
      <LayoutDashboard :size="18" /><span>概览</span>
    </RouterLink>
    <section v-for="group in groups" :key="group.label" class="navigation-group" :aria-label="group.label">
      <h2 class="navigation-group-title">{{ group.label }}</h2>
      <RouterLink v-for="item in group.items" :key="item.to" :to="item.to" class="navigation-item">
        <component :is="item.icon" :size="18" /><span>{{ item.label }}</span>
      </RouterLink>
    </section>
  </nav>
</template>
