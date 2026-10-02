import { beforeEach, describe, expect, it } from "vitest";
import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";

import NavigationMenu from "./NavigationMenu.vue";
import { useSessionStore } from "@/stores/session";

describe("NavigationMenu", () => {
  beforeEach(() => setActivePinia(createPinia()));

  it("shows shared pages but hides administration from members", () => {
    const store = useSessionStore();
    store.setWorkspaces([
      { id: 1, name: "团队", timezone: "Asia/Shanghai", role: "MEMBER" },
    ]);
    store.currentWorkspaceId = 1;
    const wrapper = mount(NavigationMenu, {
      global: { stubs: { RouterLink: { template: "<a><slot /></a>" } } },
    });
    expect(wrapper.text()).toContain("团队日历");
    expect(wrapper.text()).not.toContain("成员管理");
    const groups = wrapper.findAll(".navigation-group");
    expect(groups).toHaveLength(2);
    expect(groups[0]!.attributes("aria-label")).toBe("个人");
    expect(groups[0]!.text()).toContain("我的排班");
    expect(groups[0]!.text()).toContain("个人资料");
    expect(groups[0]!.text()).not.toContain("导入");
    expect(groups[1]!.attributes("aria-label")).toBe("工作区");
    expect(groups[1]!.text()).toContain("我的导入记录");
    expect(groups.every((group) => !group.text().includes("概览"))).toBe(true);
    expect(wrapper.get("nav > .navigation-item").text()).toBe("概览");
    wrapper.unmount();
  });

  it("shows administration pages for owners and administrators", () => {
    const store = useSessionStore();
    store.setWorkspaces([
      { id: 1, name: "团队", timezone: "Asia/Shanghai", role: "ADMIN" },
    ]);
    store.currentWorkspaceId = 1;
    const wrapper = mount(NavigationMenu, {
      global: { stubs: { RouterLink: { template: "<a><slot /></a>" } } },
    });
    expect(wrapper.text()).toContain("排班管理");
    expect(wrapper.text()).toContain("成员管理");
    expect(wrapper.text()).toContain("班次设置");
    expect(wrapper.get('[aria-label="工作区"]').text()).toContain("成员管理");
    expect(wrapper.get('[aria-label="个人"]').text()).not.toContain("成员管理");
    wrapper.unmount();
  });
});
