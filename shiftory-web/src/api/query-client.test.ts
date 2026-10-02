import { afterEach, expect, it } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { queryClient, invalidateScheduleViews } from "./query-client";
import { useSessionStore } from "../stores/session";

afterEach(() => queryClient.clear());

it("does not reuse a previous account's fresh data after logout", async () => {
  setActivePinia(createPinia());
  await queryClient.fetchQuery({ queryKey: ["my-invitations"], queryFn: async () => ["A"] });
  useSessionStore().clearSession();
  expect(await queryClient.fetchQuery({ queryKey: ["my-invitations"], queryFn: async () => ["B"] })).toEqual(["B"]);
});

it("invalidates schedules and derived views in every workspace", async () => {
  const keys = [["schedules", 1, 7], ["schedules", 2, 7], ["history", 2], ["calendar", 1], ["overview", 2], ["members", 1], ["import", 2, 3], ["imports", 1]];
  for (const key of [...keys, ["shifts", 1]]) queryClient.setQueryData(key, {});
  await invalidateScheduleViews();
  for (const key of keys) expect(queryClient.getQueryState(key)?.isInvalidated).toBe(true);
  expect(queryClient.getQueryState(["shifts", 1])?.isInvalidated).toBe(false);
});
