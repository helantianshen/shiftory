import { expect, it, vi } from "vitest";
import { mount } from "@vue/test-utils";
import { createPinia } from "pinia";
import { QueryClient, VueQueryPlugin } from "@tanstack/vue-query";
import ElementPlus from "element-plus";
import dayjs from "dayjs";
import { api } from "@/api/client";
import ScheduleBoard from "./ScheduleBoard.vue";

it("aligns dates under Monday-first weekday headings after changing months", async () => {
  const get = vi.spyOn(api, "get").mockResolvedValue({ items: [] });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = mount(ScheduleBoard, {
    props: { personal: true, userId: 1 },
    global: { plugins: [createPinia(), ElementPlus, [VueQueryPlugin, { queryClient: client }]] },
  });
  try {
    expect(wrapper.findAll(".calendar-weekdays span").map((item) => item.text())).toEqual([
      "周一", "周二", "周三", "周四", "周五", "周六", "周日",
    ]);
    const checkMonth = (month: ReturnType<typeof dayjs>) => {
      const cells = wrapper.findAll(".calendar-cell");
      expect(cells).toHaveLength(month.daysInMonth());
      for (const [index, cell] of cells.entries()) {
        const date = month.date(index + 1);
        const style = (cell.element as HTMLElement).style;
        expect(cell.get(".date").text()).toBe(String(index + 1));
        expect(style.gridColumn).toBe(String((date.day() + 6) % 7 + 1));
        expect(style.gridRow).toBe(String(Math.ceil(((month.startOf("month").day() + 6) % 7 + index + 1) / 7)));
      }
    };
    const month = dayjs().startOf("month");
    checkMonth(month);
    await wrapper.findAll(".toolbar button")[1]!.trigger("click");
    checkMonth(month.add(1, "month"));
  } finally {
    wrapper.unmount();
    client.clear();
    get.mockRestore();
  }
});
