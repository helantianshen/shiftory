import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { VueQueryPlugin } from "@tanstack/vue-query";
import ElementPlus from "element-plus";
import router from "../router";
import { queryClient } from "../api/query-client";
import { api } from "../api/client";
import { useSessionStore } from "../stores/session";
import ImportView from "./ImportView.vue";

afterEach(() => {
  queryClient.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it("renders without randomUUID and preserves the request key for retries", async () => {
  vi.stubGlobal("crypto", {
    getRandomValues: crypto.getRandomValues.bind(crypto),
  });
  const pinia = createPinia();
  setActivePinia(pinia);
  const session = useSessionStore();
  session.initialized = true;
  session.setSession(
    {
      id: 1,
      username: "a",
      email: "a@b.test",
      displayName: "A",
      avatarUrl: "",
      status: "ACTIVE",
    },
    "token",
  );
  session.setWorkspaces([
    { id: 1, name: "A", timezone: "UTC", role: "MEMBER" },
  ]);
  await router.push("/import");
  vi.spyOn(router, "push").mockResolvedValue(undefined);
  const post = vi
    .spyOn(api, "post")
    .mockRejectedValueOnce(new Error("暂时失败"))
    .mockResolvedValue({ id: 2 });
  const wrapper = mount(ImportView, {
    global: {
      plugins: [pinia, router, ElementPlus, [VueQueryPlugin, { queryClient }]],
    },
  });
  try {
    expect(wrapper.text()).toContain("导入排班");
    await wrapper
      .findAll("button")
      .find((button) => button.text().includes("描述排班规则"))!
      .trigger("click");
    await wrapper.find("textarea").setValue("每周一休息");
    const submit = () =>
      wrapper
        .findAll("button")
        .find((button) => button.text().includes("创建识别任务"))!
        .trigger("click");
    await submit();
    await flushPromises();
    const key = post.mock.calls[0]![2]!["Idempotency-Key"];
    expect(key).toMatch(/^[0-9a-f]{32}$/);
    await submit();
    await flushPromises();
    expect(post.mock.calls[1]![2]!["Idempotency-Key"]).toBe(key);
    await submit();
    await flushPromises();
    expect(post.mock.calls[2]![2]!["Idempotency-Key"]).not.toBe(key);
  } finally {
    wrapper.unmount();
  }
});
