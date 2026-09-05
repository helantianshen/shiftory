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
  });
});
