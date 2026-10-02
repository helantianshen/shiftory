import { describe, expect, it } from "vitest";
import { mount } from "@vue/test-utils";

import ScheduleEditor from "./ScheduleEditor.vue";

describe("ScheduleEditor", () => {
  it("keeps REST free of segments and emits a normalized save payload", async () => {
    const wrapper = mount(ScheduleEditor, {
      props: { date: "2026-09-04", shifts: [], modelValue: null },
    });
    await wrapper.get('[data-test="status-rest"]').trigger("click");
    await wrapper.get('[data-test="save-schedule"]').trigger("click");
    const payload = wrapper.emitted("save")?.[0]?.[0] as {
      status: string;
      segments: unknown[];
    };
    expect(payload.status).toBe("REST");
    expect(payload.segments).toEqual([]);
  });
});

it("resets unsaved state when moving between empty dates", async () => {
  const wrapper = mount(ScheduleEditor, { props: { date: "2026-10-01", shifts: [], modelValue: null } });
  await wrapper.get('[data-test="status-rest"]').trigger("click");
  await wrapper.setProps({ date: "2026-10-02", modelValue: null });
  await wrapper.get('[data-test="save-schedule"]').trigger("click");
  expect(wrapper.emitted("save")?.[0]?.[0]).toMatchObject({ status: "WORKING", note: "", version: 0, segments: [] });
  wrapper.unmount();
});

it("preserves an archived shift through its persisted segment reference", async () => {
  const wrapper = mount(ScheduleEditor, {
    props: {
      date: "2026-10-01",
      shifts: [],
      modelValue: {
        id: 1, workspaceId: 0, userId: 2, workDate: "2026-10-01",
        status: "WORKING", sourceType: "MANUAL", note: "", version: 3,
        segments: [{ id: 7, type: "SHIFT", shiftName: "早班", crossDay: false, sortOrder: 0 }],
      },
    },
  });
  expect(wrapper.text()).toContain("早班（保留原班次）");
  await wrapper.get('[data-test="save-schedule"]').trigger("click");
  expect(wrapper.emitted("save")?.[0]?.[0]).toMatchObject({
    version: 3, segments: [{ type: "SHIFT", existingSegmentId: 7 }],
  });
  wrapper.unmount();
});

it("adds a custom time range when no workspace presets are available", async () => {
  const wrapper = mount(ScheduleEditor, { props: { date: "2026-10-01", shifts: [], modelValue: null } });
  await wrapper.get(".add-segment").trigger("click");
  await wrapper.get('[data-test="save-schedule"]').trigger("click");
  expect(wrapper.emitted("save")?.[0]?.[0]).toMatchObject({ segments: [{ type: "TIME_RANGE", startTime: "08:30", endTime: "17:30" }] });
  wrapper.unmount();
});
