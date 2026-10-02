import { afterEach, expect, it } from "vitest";
import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { VueQueryPlugin } from "@tanstack/vue-query";
import ElementPlus from "element-plus";
import router from "../router";
import { queryClient } from "../api/query-client";
import { useSessionStore } from "../stores/session";
import ImportReviewView from "./ImportReviewView.vue";

afterEach(() => queryClient.clear());

it("hides every write action on a team import review", async () => {
  const pinia = createPinia();
  setActivePinia(pinia);
  const session = useSessionStore();
  session.initialized = true;
  session.setSession({ id: 1, username: "a", email: "a@b.test", displayName: "A", avatarUrl: "", status: "ACTIVE" }, "token");
  session.setWorkspaces([{ id: 1, name: "A", timezone: "UTC", role: "OWNER" }]);
  await router.push("/admin/imports/3?workspaceId=1");
  queryClient.setQueryData(["import", 1, 3], { id: 3, targetUserId: 1, state: "NEEDS_REVIEW", items: [{ id: 4, type: "CONFLICT", workDate: "2026-10-01", decision: "", draft: {status:"REST",segments:[]} }] });
  queryClient.setQueryData(["shifts", 1], {items: []});
  const wrapper = mount(ImportReviewView, { global: { plugins: [pinia, router, ElementPlus, [VueQueryPlugin, { queryClient }]] } });
  const buttons = wrapper.findAll("button").map((button) => button.text());
  for (const action of ["取消任务", "撤销导入", "保存决策", "确认并提交导入", "人工修正", "全部使用导入"]) expect(buttons).not.toContain(action);
  expect(wrapper.findAll('input[type="radio"]').every((input) => (input.element as HTMLInputElement).disabled)).toBe(true);
  wrapper.unmount();
});
