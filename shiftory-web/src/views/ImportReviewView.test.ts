import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { VueQueryPlugin } from "@tanstack/vue-query";
import ElementPlus, { ElMessageBox } from "element-plus";
import router from "../router";
import { queryClient } from "../api/query-client";
import { useSessionStore } from "../stores/session";
import { api } from "../api/client";
import ImportReviewView from "./ImportReviewView.vue";

afterEach(() => {
  queryClient.clear();
  vi.restoreAllMocks();
});

it("hides every write action on a team import review", async () => {
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
  session.setWorkspaces([{ id: 1, name: "A", timezone: "UTC", role: "OWNER" }]);
  await router.push("/admin/imports/3?workspaceId=1");
  queryClient.setQueryData(["import", 1, 3], {
    id: 3,
    targetUserId: 1,
    state: "NEEDS_REVIEW",
    items: [
      {
        id: 4,
        type: "CONFLICT",
        workDate: "2026-10-01",
        decision: "",
        draft: { status: "REST", segments: [] },
      },
    ],
  });
  queryClient.setQueryData(["shifts", 1], { items: [] });
  const wrapper = mount(ImportReviewView, {
    global: {
      plugins: [pinia, router, ElementPlus, [VueQueryPlugin, { queryClient }]],
    },
  });
  const buttons = wrapper.findAll("button").map((button) => button.text());
  for (const action of [
    "取消任务",
    "撤销导入",
    "保存决策",
    "确认并提交导入",
    "人工修正",
    "全部使用导入",
  ])
    expect(buttons).not.toContain(action);
  expect(
    wrapper
      .findAll('input[type="radio"]')
      .every((input) => (input.element as HTMLInputElement).disabled),
  ).toBe(true);
  wrapper.unmount();
});

it("shows all text draft segments and hides the original-file action", async () => {
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
  session.setWorkspaces([{ id: 1, name: "A", timezone: "UTC", role: "OWNER" }]);
  await router.push("/imports/4?workspaceId=1");
  queryClient.setQueryData(["import", 1, 4], {
    id: 4,
    targetUserId: 1,
    importType: "TEXT_AI",
    state: "NEEDS_REVIEW",
    reviewVersion: 2,
    description: "每周一上班",
    items: [
      {
        id: 5,
        type: "NEW",
        workDate: "2026-10-01",
        decision: "",
        draft: {
          status: "WORKING",
          segments: [
            {
              type: "TIME_RANGE",
              startTime: "09:00",
              endTime: "12:00",
              crossDay: false,
            },
            {
              type: "TIME_RANGE",
              startTime: "22:00",
              endTime: "06:00",
              crossDay: true,
            },
          ],
        },
      },
    ],
  });
  queryClient.setQueryData(["shifts", 1], { items: [] });
  const wrapper = mount(ImportReviewView, {
    global: {
      plugins: [pinia, router, ElementPlus, [VueQueryPlugin, { queryClient }]],
    },
  });
  await flushPromises();
  expect(wrapper.text()).toContain("09:00–12:00；22:00–06:00（次日）");
  expect(wrapper.text()).toContain("每周一上班");
  expect(wrapper.findAll("button").map((b) => b.text())).not.toContain(
    "下载原文件",
  );
  wrapper.unmount();
});

it("commits using the review version returned after saving decisions", async () => {
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
  session.setWorkspaces([{ id: 1, name: "A", timezone: "UTC", role: "OWNER" }]);
  await router.push("/imports/5?workspaceId=1");
  const fixture = {
    id: 5,
    targetUserId: 1,
    importType: "TEXT_AI",
    state: "NEEDS_REVIEW",
    reviewVersion: 7,
    items: [
      {
        id: 6,
        type: "NEW",
        workDate: "2026-10-01",
        decision: "SKIP",
        draft: { status: "REST", segments: [] },
      },
    ],
  };
  queryClient.setQueryData(["import", 1, 5], fixture);
  queryClient.setQueryData(["shifts", 1], { items: [] });
  vi.spyOn(api, "get").mockResolvedValue({
    ...fixture,
    reviewVersion: 8,
  } as never);
  const put = vi
    .spyOn(api, "put")
    .mockResolvedValue({ reviewVersion: 8 } as never);
  const post = vi
    .spyOn(api, "post")
    .mockResolvedValue({ writeCount: 0 } as never);
  vi.spyOn(ElMessageBox, "confirm").mockResolvedValue("confirm" as never);
  const wrapper = mount(ImportReviewView, {
    global: {
      plugins: [pinia, router, ElementPlus, [VueQueryPlugin, { queryClient }]],
    },
  });
  const button = wrapper
    .findAll("button")
    .find((b) => b.text() === "确认并提交导入");
  expect(button).toBeDefined();
  await button!.trigger("click");
  await flushPromises();
  expect(put).toHaveBeenCalledWith(
    "/workspaces/1/imports/5/decisions",
    expect.objectContaining({ expectedReviewVersion: 7 }),
  );
  expect(post).toHaveBeenCalledWith("/workspaces/1/imports/5/commit", {
    expectedReviewVersion: 8,
  });
  wrapper.unmount();
});
