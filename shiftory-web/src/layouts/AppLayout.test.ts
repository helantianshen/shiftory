import { afterEach, expect, it } from "vitest";
import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { VueQueryPlugin } from "@tanstack/vue-query";
import ElementPlus from "element-plus";
import router from "../router";
import { queryClient } from "../api/query-client";
import { useSessionStore } from "../stores/session";
import AppLayout from "./AppLayout.vue";
import ScheduleView from "../views/ScheduleView.vue";

afterEach(() => queryClient.clear());

it("shows workspace scope only on workspace pages", async () => {
  const pinia = createPinia();
  setActivePinia(pinia);
  const session = useSessionStore();
  session.initialized = true;
  session.setSession({ id: 1, username: "a", email: "a@b.test", displayName: "A", avatarUrl: "", status: "ACTIVE" }, "token");
  session.setWorkspaces([{ id: 1, name: "开发工作区", timezone: "UTC", role: "OWNER" }]);
  queryClient.setQueryData(["my-invitations"], { items: [] });
  await router.push("/schedule");
  const wrapper = mount(AppLayout, { global: { plugins: [pinia, router, ElementPlus, [VueQueryPlugin, { queryClient }]], stubs: { RouterView: true } } });
  expect(wrapper.find(".workspace-switcher").exists()).toBe(false);
  expect(wrapper.find(".profile-copy small").text()).toBe("个人账号");
  await router.push("/calendar");
  expect(wrapper.get(".workspace-switcher").text()).toContain("开发工作区");
  expect(wrapper.find(".profile-copy small").text()).toBe("OWNER");
  await router.push("/profile");
  expect(wrapper.find(".workspace-switcher").exists()).toBe(false);
  wrapper.unmount();
});

it("renders the personal schedule without requiring workspace membership", () => {
  const pinia = createPinia();
  setActivePinia(pinia);
  const session = useSessionStore();
  session.setSession({ id: 1, username: "a", email: "a@b.test", displayName: "A", avatarUrl: "", status: "ACTIVE" }, "token");
  const wrapper = mount(ScheduleView, { global: { plugins: [pinia], stubs: { ScheduleBoard: true } } });
  expect(wrapper.text()).toContain("我的排班");
  expect(wrapper.findComponent({ name: "ScheduleBoard" }).props("personal")).toBe(true);
  expect(wrapper.text()).not.toContain("创建工作区");
  wrapper.unmount();
});
