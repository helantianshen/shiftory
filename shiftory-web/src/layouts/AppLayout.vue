<script setup lang="ts">
import { computed, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Bell, ChevronDown, Menu, Sparkles } from "lucide-vue-next";
import NavigationMenu from "@/components/NavigationMenu.vue";
import { useSessionStore, type ThemeName } from "@/stores/session";
import { sidebarThemeColor } from "@/styles/themes";
import { api } from "@/api/client";
import { useQuery } from "@tanstack/vue-query";
const session = useSessionStore();
const route = useRoute();
const router = useRouter();
const workspaceScoped = computed(() => ['/overview', '/calendar', '/admin/schedules', '/admin/members', '/admin/workspace', '/admin/imports'].some((path) => route.path === path || route.path.startsWith(`${path}/`)));
const pendingInvitations = useQuery({ queryKey: ["my-invitations"], queryFn: () => api.get<{items: unknown[]}>("/invitations/mine") });
const sidebarOpen = ref(false);
const themes: { value: ThemeName; label: string; color: string }[] = [
  { value: "mint", label: "薄荷", color: "#3aa981" },
  { value: "sky", label: "晴空", color: "#458bd8" },
  { value: "lilac", label: "丁香", color: "#826fc4" },
  { value: "sakura", label: "樱粉", color: "#cf6f8c" },
  { value: "amber", label: "琥珀", color: "#c88632" },
  { value: "graphite", label: "石墨", color: "#68727f" },
];
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
    <aside
      class="sidebar"
      :class="{ open: sidebarOpen }"
      :style="{ '--sidebar-bg': sidebarThemeColor(session.theme) }"
    >
      <RouterLink to="/overview" class="brand"
        ><span class="brand-mark"><Sparkles :size="21" /></span
        ><span>Shiftory<small>排班协同</small></span></RouterLink
      ><NavigationMenu />
      <div class="sidebar-footer">
        清晰排班，轻松协同<span>工作区时区独立生效</span>
      </div>
    </aside>
    <div v-if="sidebarOpen" class="sidebar-mask" @click="sidebarOpen = false" />
    <section class="shell-content">
      <header class="topbar">
        <button
          class="mobile-menu"
          aria-label="打开菜单"
          @click="sidebarOpen = true"
        >
          <Menu />
        </button>
        <el-dropdown
          v-if="session.workspaces.length && workspaceScoped"
          trigger="click"
          @command="session.selectWorkspace"
          ><button class="workspace-switcher">
            <span class="workspace-dot" />{{ session.currentWorkspace?.name
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
        <span v-else-if="workspaceScoped" class="workspace-switcher">尚未创建工作区</span>
        <div class="topbar-spacer" />
        <el-dropdown
          trigger="click"
          @command="(value: ThemeName) => session.selectTheme(value)"
          ><button class="icon-button" aria-label="切换主题">
            <span class="theme-swatch" /><ChevronDown :size="13" /></button
          ><template #dropdown
            ><el-dropdown-menu
              ><el-dropdown-item
                v-for="theme in themes"
                :key="theme.value"
                :command="theme.value"
                ><span
                  class="dropdown-swatch"
                  :style="{ background: theme.color }"
                />{{ theme.label }}</el-dropdown-item
              ></el-dropdown-menu
            ></template
          ></el-dropdown
        >
        <button class="icon-button" aria-label="通知" @click="router.push('/invitations')">
          <el-badge :value="pendingInvitations.data.value?.items.length || 0" :hidden="!pendingInvitations.data.value?.items.length"><Bell :size="18" /></el-badge>
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
                session.currentWorkspace?.role ?? "成员"
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
        :key="`${route.path}-${session.currentWorkspaceId}`"
        class="page-container"
      >
        <RouterView />
      </main>
    </section>
  </div>
</template>
