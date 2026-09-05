<script setup lang="ts">
import { computed, ref } from "vue";
import { useQuery, useQueryClient } from "@tanstack/vue-query";
import dayjs, { type Dayjs } from "dayjs";
import { ArrowLeft, ArrowRight, History, Layers3 } from "lucide-vue-next";
import { ElMessage } from "element-plus";
import { api } from "@/api/client";
import type { ScheduleDay, Shift } from "@/api/types";
import ScheduleEditor from "./ScheduleEditor.vue";
const props = defineProps<{ workspaceId: number; userId: number }>();
const month = ref<Dayjs>(dayjs().startOf("month"));
const editorOpen = ref(false);
const selectedDate = ref(dayjs().format("YYYY-MM-DD"));
const historyOpen = ref(false);
const batchOpen = ref(false);
const batchDates = ref<string[]>([]);
const batchStatus = ref<"WORKING" | "REST">("REST");
const queryClient = useQueryClient();
const start = computed(() => month.value.startOf("month").format("YYYY-MM-DD"));
const end = computed(() => month.value.endOf("month").format("YYYY-MM-DD"));
const schedules = useQuery({
  queryKey: computed(() => [
    "schedules",
    props.workspaceId,
    props.userId,
    start.value,
    end.value,
  ]),
  queryFn: () =>
    api.get<{ items: ScheduleDay[] }>(
      `/workspaces/${props.workspaceId}/schedules/${props.userId}?start=${start.value}&end=${end.value}`,
    ),
});
const shifts = useQuery({
  queryKey: computed(() => ["shifts", props.workspaceId]),
  queryFn: () =>
    api.get<{ items: Shift[] }>(`/workspaces/${props.workspaceId}/shifts`),
});
const selected = computed(
  () =>
    schedules.data.value?.items.find(
      (item) => item.workDate === selectedDate.value,
    ) ?? null,
);
const calendar = computed(() => {
  const result: { date: string; day: number; schedule: ScheduleDay | null }[] =
    [];
  const count = month.value.daysInMonth();
  for (let day = 1; day <= count; day++) {
    const date = month.value.date(day).format("YYYY-MM-DD");
    result.push({
      date,
      day,
      schedule:
        schedules.data.value?.items.find((item) => item.workDate === date) ??
        null,
    });
  }
  return result;
});
const history = useQuery({
  queryKey: computed(() => [
    "history",
    props.workspaceId,
    props.userId,
    selectedDate.value,
  ]),
  queryFn: () =>
    api.get<{
      items: {
        id: number;
        changeType: string;
        changedBy: number;
        createdAt: string;
      }[];
    }>(
      `/workspaces/${props.workspaceId}/schedules/${props.userId}/${selectedDate.value}/history`,
    ),
  enabled: computed(() => historyOpen.value),
});
function edit(date: string) {
  selectedDate.value = date;
  editorOpen.value = true;
}
async function save(payload: unknown) {
  await api.put(
    `/workspaces/${props.workspaceId}/schedules/${props.userId}/${selectedDate.value}`,
    payload,
  );
  ElMessage.success("排班已保存");
  editorOpen.value = false;
  await queryClient.invalidateQueries({
    queryKey: ["schedules", props.workspaceId, props.userId],
  });
}
async function saveBatch() {
  if (!batchDates.value.length) return;
  await api.post(`/workspaces/${props.workspaceId}/schedules/batch`, {
    userId: props.userId,
    dates: batchDates.value,
    status: batchStatus.value,
    note: "",
    segments: [],
  });
  ElMessage.success(`已更新 ${batchDates.value.length} 天`);
  batchOpen.value = false;
  await queryClient.invalidateQueries({
    queryKey: ["schedules", props.workspaceId, props.userId],
  });
}
</script>
<template>
  <section>
    <div class="toolbar">
      <el-button circle @click="month = month.subtract(1, 'month')"
        ><ArrowLeft :size="16" /></el-button
      ><strong style="min-width: 110px; text-align: center">{{
        month.format("YYYY年 M月")
      }}</strong
      ><el-button circle @click="month = month.add(1, 'month')"
        ><ArrowRight :size="16" /></el-button
      ><el-button @click="batchOpen = true"
        ><Layers3 :size="16" />批量设置</el-button
      >
    </div>
    <div class="surface-card card-padding">
      <div class="schedule-grid">
        <button
          v-for="cell in calendar"
          :key="cell.date"
          class="calendar-cell"
          :class="[
            { rest: cell.schedule?.status === 'REST', missing: !cell.schedule },
          ]"
          @click="edit(cell.date)"
        >
          <span class="date">{{ cell.day }}</span
          ><span class="badge">{{
            cell.schedule?.status === "REST"
              ? "休息"
              : (cell.schedule?.segments[0]?.shiftName ??
                (cell.schedule ? "工作" : "未排班"))
          }}</span
          ><small
            v-if="cell.schedule"
            style="display: block; margin-top: 7px; color: var(--muted)"
            >{{ cell.schedule.sourceType }}</small
          >
        </button>
      </div>
    </div>
    <el-drawer v-model="editorOpen" title="编辑日排班" size="min(520px, 96vw)"
      ><ScheduleEditor
        :date="selectedDate"
        :shifts="shifts.data.value?.items ?? []"
        :model-value="selected"
        @save="save"
      /><el-button
        v-if="selected"
        text
        style="margin-top: 18px"
        @click="historyOpen = true"
        ><History :size="16" />查看修改历史</el-button
      ></el-drawer
    ><el-drawer v-model="historyOpen" title="修改历史" size="min(480px, 96vw)"
      ><el-timeline
        ><el-timeline-item
          v-for="item in history.data.value?.items ?? []"
          :key="item.id"
          :timestamp="dayjs(item.createdAt).format('YYYY-MM-DD HH:mm')"
          ><strong>{{ item.changeType }}</strong>
          <div class="muted">
            操作人 #{{ item.changedBy }}
          </div></el-timeline-item
        ></el-timeline
      ></el-drawer
    ><el-dialog
      v-model="batchOpen"
      title="批量设置排班"
      width="min(520px, 94vw)"
      ><el-form label-position="top"
        ><el-form-item label="日期"
          ><el-date-picker
            v-model="batchDates"
            type="dates"
            value-format="YYYY-MM-DD"
            placeholder="选择多个日期" /></el-form-item
        ><el-form-item label="状态"
          ><el-radio-group v-model="batchStatus"
            ><el-radio-button value="WORKING">工作</el-radio-button
            ><el-radio-button value="REST"
              >休息</el-radio-button
            ></el-radio-group
          ></el-form-item
        ></el-form
      ><template #footer
        ><el-button @click="batchOpen = false">取消</el-button
        ><el-button type="primary" @click="saveBatch">应用</el-button></template
      ></el-dialog
    >
  </section>
</template>
