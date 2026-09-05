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
