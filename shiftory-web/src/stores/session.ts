import { computed, ref } from "vue";
import { defineStore } from "pinia";

import { api } from "@/api/client";

export type ThemeName =
  "mint" | "sky" | "lilac" | "sakura" | "amber" | "graphite";

export interface User {
  id: number;
  username: string;
  email: string;
  displayName: string;
  avatarUrl: string;
  status: string;
}

export interface Workspace {
  id: number;
  name: string;
  timezone: string;
  role: "OWNER" | "ADMIN" | "MEMBER";
}

export interface Preferences {
  currentWorkspaceId: number | null;
  theme: ThemeName;
}

export const useSessionStore = defineStore("session", () => {
  const user = ref<User | null>(null);
  const accessToken = ref<string | null>(null);
  const workspaces = ref<Workspace[]>([]);
  const currentWorkspaceId = ref<number | null>(null);
  const theme = ref<ThemeName>("mint");
  const initialized = ref(false);

  const currentWorkspace = computed(
    () =>
      workspaces.value.find(
        (workspace) => workspace.id === currentWorkspaceId.value,
      ) ??
      workspaces.value[0] ??
      null,
  );
  const isAuthenticated = computed(
    () => user.value !== null && accessToken.value !== null,
  );
  const isAdmin = computed(
    () =>
      currentWorkspace.value?.role === "OWNER" ||
      currentWorkspace.value?.role === "ADMIN",
  );
  const isOwner = computed(() => currentWorkspace.value?.role === "OWNER");

  function setSession(nextUser: User, token: string) {
    user.value = nextUser;
    accessToken.value = token;
  }

  function setAccessToken(token: string | null) {
    accessToken.value = token;
  }

  function setWorkspaces(next: Workspace[]) {
    workspaces.value = next;
    if (!next.some((workspace) => workspace.id === currentWorkspaceId.value))
      currentWorkspaceId.value = next[0]?.id ?? null;
  }

  function applyPreferences(preferences: Preferences) {
    theme.value = preferences.theme;
    currentWorkspaceId.value = preferences.currentWorkspaceId;
    document.documentElement.dataset.theme = preferences.theme;
  }

  function clearSession() {
    user.value = null;
    accessToken.value = null;
    workspaces.value = [];
    currentWorkspaceId.value = null;
  }

  async function loadContext() {
    const [nextUser, workspaceData, preferences] = await Promise.all([
      api.get<User>("/auth/me"),
      api.get<{ items: Workspace[] }>("/workspaces"),
      api.get<Preferences>("/preferences"),
    ]);
    user.value = nextUser;
    setWorkspaces(workspaceData.items);
    applyPreferences(preferences);
  }

  async function bootstrap() {
    if (initialized.value) return;
    try {
      await api.refresh();
      await loadContext();
    } catch {
      clearSession();
    } finally {
      initialized.value = true;
    }
  }

  async function login(loginValue: string, password: string) {
    const result = await api.post<{ accessToken: string; user: User }>(
      "/auth/login",
      { login: loginValue, password },
    );
    setSession(result.user, result.accessToken);
    await loadContext();
  }

  async function register(input: {
    username: string;
    email: string;
    displayName: string;
    password: string;
  }) {
    await api.post<User>("/auth/register", input);
    await login(input.email, input.password);
  }

  async function logout() {
    try {
      await api.post("/auth/logout", {});
    } finally {
      clearSession();
    }
  }

  async function selectWorkspace(id: number) {
    currentWorkspaceId.value = id;
    await api.put<Preferences>("/preferences", {
      currentWorkspaceId: id,
      theme: theme.value,
    });
  }

  async function selectTheme(next: ThemeName) {
    theme.value = next;
    document.documentElement.dataset.theme = next;
    await api.put<Preferences>("/preferences", {
      currentWorkspaceId: currentWorkspaceId.value,
      theme: next,
    });
  }

  return {
    user,
    accessToken,
    workspaces,
    currentWorkspaceId,
    theme,
    initialized,
    currentWorkspace,
    isAuthenticated,
    isAdmin,
    isOwner,
    setSession,
    setAccessToken,
    setWorkspaces,
    applyPreferences,
    clearSession,
    loadContext,
    bootstrap,
    login,
    register,
    logout,
    selectWorkspace,
    selectTheme,
  };
});
