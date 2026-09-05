<script setup lang="ts">
import { computed, reactive, watch } from "vue";
import { Plus, Trash2 } from "lucide-vue-next";
import type { ScheduleDay, ScheduleSegment, Shift } from "@/api/types";

const props = defineProps<{
  date: string;
  shifts: Shift[];
  modelValue: ScheduleDay | null;
}>();
const emit = defineEmits<{
  save: [
    payload: {
      status: "WORKING" | "REST";
      note: string;
      version: number;
      segments: Omit<ScheduleSegment, "id">[];
    },
  ];
}>();
type EditorSegment = {
  type: "SHIFT" | "TIME_RANGE";
  shiftId?: number;
  startTime?: string;
  endTime?: string;
  crossDay: boolean;
};
const form = reactive<{
  status: "WORKING" | "REST";
  note: string;
  version: number;
  segments: EditorSegment[];
}>({ status: "WORKING", note: "", version: 0, segments: [] });
watch(
  () => props.modelValue,
  (value) => {
    form.status = value?.status ?? "WORKING";
    form.note = value?.note ?? "";
    form.version = value?.version ?? 0;
    form.segments =
      value?.segments.map((segment) => ({
        type: segment.type,
        shiftId: segment.shiftId,
        startTime: segment.startTime,
        endTime: segment.endTime,
        crossDay: segment.crossDay,
      })) ?? [];
  },
  { immediate: true },
);
const canAdd = computed(
  () => form.status === "WORKING" && form.segments.length < 8,
);
function chooseStatus(status: "WORKING" | "REST") {
  form.status = status;
  if (status === "REST") form.segments = [];
}
function addSegment() {
  form.segments.push({ type: "SHIFT", crossDay: false });
}
function switchType(segment: EditorSegment) {
  segment.shiftId = undefined;
  segment.startTime = segment.type === "TIME_RANGE" ? "08:30" : undefined;
  segment.endTime = segment.type === "TIME_RANGE" ? "17:30" : undefined;
}
function save() {
  emit("save", {
    status: form.status,
    note: form.note.trim(),
    version: form.version,
    segments:
      form.status === "REST"
        ? []
        : form.segments.map((segment, sortOrder) => ({
            ...segment,
            sortOrder,
          })),
  });
}
</script>
<template>
  <div class="schedule-editor">
    <div class="editor-date">{{ date }}</div>
    <div class="status-toggle">
      <button
        data-test="status-working"
        :class="{ active: form.status === 'WORKING' }"
        @click="chooseStatus('WORKING')"
      >
        工作</button
      ><button
        data-test="status-rest"
        :class="{ active: form.status === 'REST' }"
        @click="chooseStatus('REST')"
      >
        休息
      </button>
    </div>
    <div v-if="form.status === 'WORKING'" class="segment-list">
      <div v-if="form.segments.length === 0" class="subtle-card">
        仅记录“工作”，暂不填写具体班次。
      </div>
      <div
        v-for="(segment, index) in form.segments"
        :key="index"
        class="segment-row"
      >
        <el-select
          v-model="segment.type"
          aria-label="时间段类型"
          @change="switchType(segment)"
          ><el-option label="预设班次" value="SHIFT" /><el-option
            label="自定义时间"
            value="TIME_RANGE"
        /></el-select>
        <el-select
          v-if="segment.type === 'SHIFT'"
          v-model="segment.shiftId"
          placeholder="选择班次"
          ><el-option
            v-for="shift in shifts.filter((item) => item.enabled !== false)"
            :key="shift.id"
            :label="shift.name"
            :value="shift.id"
        /></el-select>
        <template v-else
          ><el-time-select
            v-model="segment.startTime"
            start="00:00"
            step="00:15"
            end="23:45"
            placeholder="开始"
          /><el-time-select
            v-model="segment.endTime"
            start="00:00"
            step="00:15"
            end="23:45"
            placeholder="结束"
          /><el-checkbox v-model="segment.crossDay">跨日</el-checkbox></template
        >
        <el-button
          circle
          text
          aria-label="删除时间段"
          @click="form.segments.splice(index, 1)"
          ><Trash2 :size="17"
        /></el-button>
      </div>
      <el-button v-if="canAdd" text class="add-segment" @click="addSegment"
        ><Plus :size="16" /> 添加时间段</el-button
      >
    </div>
    <el-form-item label="备注"
      ><el-input
        v-model="form.note"
        type="textarea"
        :rows="3"
        maxlength="1000"
        show-word-limit
    /></el-form-item>
    <el-button data-test="save-schedule" type="primary" @click="save"
      >保存排班</el-button
    >
  </div>
</template>
