<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  Bell,
  ChevronDown,
  ChevronRight,
  Menu,
  CalendarCheck,
  Moon,
  Sun,
  X,
} from "lucide-vue-next";
import NavigationMenu from "@/components/NavigationMenu.vue";
import { useSessionStore } from "@/stores/session";
import { darkMode, toggleColorMode } from "@/styles/themes";
import { ElMessage } from "element-plus";
import { api } from "@/api/client";
import { useQuery } from "@tanstack/vue-query";
const session = useSessionStore();
const route = useRoute();
const router = useRouter();
const workspaceScoped = computed(() => route.meta.dataScope === "workspace");
// 工作区变化不会触发路由守卫，管理权限需要随会话状态同步检查
watch(
  () => [route.meta.admin, session.isAdmin],
  () => {
    if (route.meta.admin && !session.isAdmin) void router.replace("/overview");
  },
);

// 导入详情属于固定工作区，切换后返回列表以免复用其他工作区的任务 ID
async function selectWorkspace(id: number) {
  try {
    await session.selectWorkspace(id);
    if (route.params.id && route.path.includes("/imports/"))
      await router.replace(
        route.meta.readOnly && session.isAdmin ? "/admin/imports" : "/imports",
      );
  } catch {
    ElMessage.error("工作区切换失败，请重试");
  }
}

const pendingInvitations = useQuery({
  queryKey: ["my-invitations"],
  queryFn: () => api.get<{ items: unknown[] }>("/invitations/mine"),
});
const sidebarOpen = ref(false);
const pageTitle = computed(() => {
  const path = route.path;
  if (path.includes("/imports/")) return "导入详情";
  const titles: Record<string, string> = {
    "/overview": "概览",
    "/schedule": "我的排班",
    "/profile": "个人资料",
    "/import": "导入排班",
    "/calendar": "团队日历",
    "/imports": "我的导入记录",
    "/invitations": "我的邀请",
    "/admin/schedules": "排班管理",
    "/admin/members": "成员管理",
    "/admin/shifts": "班次设置",
    "/admin/imports": "全部导入记录",
    "/admin/workspace": "工作区设置",
  };
  return titles[path];
});
watch(
  () => route.path,
  () => {
    sidebarOpen.value = false;
  },
);
const initials = computed(
  () => session.user?.displayName.slice(0, 1).toUpperCase() ?? "S",
);
async function signOut() {
  await session.logout();
  await router.replace("/auth");
}
</script>
<template>
  <div class="app-shell">
    <aside class="sidebar" :class="{ open: sidebarOpen }">
      <RouterLink to="/overview" class="brand"
        ><span class="brand-mark"><CalendarCheck :size="23" /></span
        ><span>Shiftory<small>排班协同</small></span></RouterLink
      ><NavigationMenu />
      <button
        class="mobile-close icon-button"
        aria-label="关闭菜单"
        @click="sidebarOpen = false"
      >
        <X :size="18" />
      </button>
      <div class="sidebar-footer">
        <el-dropdown
          v-if="session.workspaces.length > 1 && workspaceScoped"
          trigger="click"
          @command="selectWorkspace"
          ><button class="workspace-switcher">
            {{ session.currentWorkspace?.name
            }}<ChevronDown :size="15" /></button
          ><template #dropdown
            ><el-dropdown-menu
              ><el-dropdown-item
                v-for="workspace in session.workspaces"
                :key="workspace.id"
                :command="workspace.id"
                >{{ workspace.name }} · {{ workspace.role }}</el-dropdown-item
              ></el-dropdown-menu
            ></template
          ></el-dropdown
        >

        清晰排班，轻松协同<span>个人排班共享，团队按工作区查看</span>
      </div>
    </aside>
    <div v-if="sidebarOpen" class="sidebar-mask" @click="sidebarOpen = false" />
    <section class="shell-content">
      <header class="topbar">
        <button
          class="mobile-menu icon-button"
          aria-label="打开菜单"
          @click="sidebarOpen = true"
        >
          <Menu />
        </button>
        <div class="breadcrumb">
          <span>{{ workspaceScoped ? "工作区" : "个人" }}</span
          ><ChevronRight :size="13" /><strong>{{ pageTitle }}</strong>
        </div>
        <div class="topbar-spacer" />
        <button
          class="icon-button"
          :aria-label="darkMode ? '切换浅色模式' : '切换深色模式'"
          :title="darkMode ? '切换浅色模式' : '切换深色模式'"
          @click="toggleColorMode"
        >
          <Sun v-if="darkMode" :size="19" /><Moon v-else :size="19" />
        </button>
        <button
          class="icon-button"
          aria-label="通知"
          @click="router.push('/invitations')"
        >
          <el-badge
            :value="pendingInvitations.data.value?.items.length || 0"
            :hidden="!pendingInvitations.data.value?.items.length"
            ><Bell :size="18"
          /></el-badge>
        </button>
        <el-dropdown
          trigger="click"
          @command="
            (command: string) =>
              command === 'logout' ? signOut() : router.push('/profile')
          "
          ><button class="profile-button">
            <span class="avatar">{{ initials }}</span
            ><span class="profile-copy"
              ><strong>{{ session.user?.displayName }}</strong
              ><small>{{
                workspaceScoped
                  ? (session.currentWorkspace?.role ?? "成员")
                  : "个人账号"
              }}</small></span
            ><ChevronDown :size="14" /></button
          ><template #dropdown
            ><el-dropdown-menu
              ><el-dropdown-item command="profile">个人资料</el-dropdown-item
              ><el-dropdown-item divided command="logout"
                >退出登录</el-dropdown-item
              ></el-dropdown-menu
            ></template
          ></el-dropdown
        >
      </header>
      <main
        :key="
          workspaceScoped
            ? `${route.path}-${session.currentWorkspaceId}`
            : route.path
        "
        class="page-container"
      >
        <RouterView />
      </main>
    </section>
  </div>
</template>
