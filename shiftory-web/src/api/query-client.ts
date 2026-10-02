import { QueryClient } from "@tanstack/vue-query";

// 应用共用同一个缓存实例，会话结束时清除全部账号数据
export const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: 30_000, retry: 1 } },
});

// 正式排班全局共享，变更后同时刷新各工作区的派生视图
export async function invalidateScheduleViews() {
  await queryClient.invalidateQueries({
    predicate: (query) => ["schedules", "history", "calendar", "overview", "members", "imports", "import"].includes(String(query.queryKey[0])),
  });
}
