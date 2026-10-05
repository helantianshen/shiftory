import { expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { QueryClient, VueQueryPlugin } from "@tanstack/vue-query";
import ElementPlus from "element-plus";
import { api } from "@/api/client";
import { useSessionStore } from "@/stores/session";
import CalendarView from "./CalendarView.vue";

it("shows all members initially and after clearing the member filter", async () => {
  const pinia = createPinia();
  setActivePinia(pinia);
  useSessionStore().setWorkspaces([
    { id: 1, name: "团队", timezone: "Asia/Shanghai", role: "OWNER" },
  ]);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const get = vi.spyOn(api, "get").mockImplementation(async (path) => {
    if (path.endsWith("/members")) return { items: [
      { id: 1, displayName: "甲", status: "ACTIVE" },
      { id: 2, displayName: "乙", status: "ACTIVE" },
    ] };
    return { days: [{ date: "2026-10-01", working: path.includes("memberIds=") ? 1 : 2,
      rest: 0, missing: 0, allRest: false, members: [] }] };
  });
  const wrapper = mount(CalendarView, { global: {
    plugins: [pinia, ElementPlus, [VueQueryPlugin, { queryClient: client }]],
  } });
  try {
    await vi.waitFor(() => expect(wrapper.get(".calendar-cell").text()).toContain("工作 2"));
    expect(wrapper.findAll(".calendar-weekdays span").map((item) => item.text())).toEqual([
      "周一", "周二", "周三", "周四", "周五", "周六", "周日",
    ]);
    expect(wrapper.get(".calendar-cell .date").text()).toBe("1");
    expect((wrapper.get(".calendar-cell").element as HTMLElement).style.gridColumn).toBe("4");
    expect((wrapper.get(".calendar-cell").element as HTMLElement).style.gridRow).toBe("1");
    expect(get.mock.calls.some(([path]) => path.includes("/calendar?") && !path.includes("memberIds="))).toBe(true);
    const select = wrapper.findComponent({ name: "ElSelect" });
    expect(select.props("modelValue")).toEqual([]);
    select.vm.$emit("update:modelValue", [1]);
    await flushPromises();
    await vi.waitFor(() => expect(wrapper.get(".calendar-cell").text()).toContain("工作 1"));
    expect(get.mock.calls.some(([path]) => path.includes("memberIds=1"))).toBe(true);
    select.vm.$emit("update:modelValue", []);
    await flushPromises();
    await vi.waitFor(() => expect(wrapper.get(".calendar-cell").text()).toContain("工作 2"));
  } finally {
    wrapper.unmount();
    client.clear();
    get.mockRestore();
  }
});
