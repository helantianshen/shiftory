import { beforeEach, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import router from "./index";
import { useSessionStore } from "../stores/session";
import { api } from "../api/client";

beforeEach(() => {
  vi.restoreAllMocks();
  setActivePinia(createPinia());
  const session = useSessionStore();
  session.initialized = true;
  session.setSession({ id: 1, username: "user", email: "a@b.test", displayName: "A", avatarUrl: "", status: "ACTIVE" }, "token");
  session.setWorkspaces([{ id: 1, name: "A", timezone: "UTC", role: "OWNER" }, { id: 2, name: "B", timezone: "UTC", role: "MEMBER" }]);
});

it("keeps the team import detail explicitly read-only", async () => {
  await router.push("/admin/imports/10?workspaceId=1");
  expect(router.currentRoute.value.meta.readOnly).toBe(true);
});

it("resolves the workspace carried by an import link", async () => {
  vi.spyOn(api, "put").mockResolvedValue({});
  await router.push("/imports/12?workspaceId=2");
  expect(useSessionStore().currentWorkspaceId).toBe(2);
  expect(router.currentRoute.value.params.id).toBe("12");
});

it("rejects a team detail in a workspace where the account is only a member", async () => {
  vi.spyOn(api, "put").mockResolvedValue({});
  await router.push("/admin/imports/14?workspaceId=2");
  expect(router.currentRoute.value.path).toBe("/overview");
});

it("keeps the selected workspace when preference persistence fails", async () => {
  vi.spyOn(api, "put").mockRejectedValue(new Error("offline"));
  await expect(useSessionStore().selectWorkspace(2)).rejects.toThrow("offline");
  expect(useSessionStore().currentWorkspaceId).toBe(1);
});
