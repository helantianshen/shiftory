<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useQuery } from "@tanstack/vue-query";
import dayjs, { type Dayjs } from "dayjs";
import { ArrowLeft, ArrowRight, Users } from "lucide-vue-next";
import { api } from "@/api/client";
import type { CalendarDay, CalendarMemberDay, Member } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyWorkspace from "@/components/EmptyWorkspace.vue";
import { useSessionStore } from "@/stores/session";

const session = useSessionStore();
const workspaceID = computed(() => session.currentWorkspace?.id);
const month = ref<Dayjs>(dayjs().startOf("month"));
const selectedMembers = ref<number[]>([]);
const onlyAllRest = ref(false);
const onlyMissing = ref(false);
const drawer = ref(false);
const selectedDay = ref<CalendarDay>();
const members = useQuery({
  queryKey: computed(() => ["members", workspaceID.value]),
  queryFn: () =>
    api.get<{ items: Member[] }>(`/workspaces/${workspaceID.value}/members`),
  enabled: computed(() => Boolean(workspaceID.value)),
});
watch(
  () => members.data.value,
  (value) => {
    if (value && !selectedMembers.value.length)
      selectedMembers.value = value.items
        .filter((item) => item.status === "ACTIVE")
        .map((item) => item.id);
  },
  { immediate: true },
);
const query = useQuery({
  queryKey: computed(() => [
    "calendar",
    workspaceID.value,
    month.value.format("YYYY-MM"),
    selectedMembers.value.join(","),
  ]),
  queryFn: () =>
    api.get<{ days: CalendarDay[] }>(
      `/workspaces/${workspaceID.value}/calendar?start=${month.value.startOf("month").format("YYYY-MM-DD")}&end=${month.value.endOf("month").format("YYYY-MM-DD")}&memberIds=${selectedMembers.value.join(",")}`,
    ),
  enabled: computed(() =>
    Boolean(workspaceID.value && selectedMembers.value.length),
  ),
});
const days = computed(
  () =>
    query.data.value?.days.filter(
      (day) =>
        (!onlyAllRest.value || day.allRest) &&
        (!onlyMissing.value || day.missing > 0),
    ) ?? [],
);
const today = computed(() => dayjs().format("YYYY-MM-DD"));
const memberName = (id: number) =>
  members.data.value?.items.find((item) => item.id === id)?.displayName ??
  `成员 #${id}`;
function open(day: CalendarDay) {
  selectedDay.value = day;
  drawer.value = true;
}
function scheduleDetail(member: CalendarMemberDay) {
  if (member.status === "MISSING") return "排班缺失";
  if (member.status === "REST")
    return member.note ? `休息 · ${member.note}` : "休息";
  const segments = member.segments
    .map((segment) =>
      segment.shiftName
        ? segment.shiftName
        : `${segment.startTime ?? "?"}–${segment.endTime ?? "?"}${segment.crossDay ? "（跨日）" : ""}`,
    )
    .join(" / ");
  return `${segments || "工作"}${member.note ? ` · ${member.note}` : ""}`;
}
</script>

<template>
  <EmptyWorkspace v-if="!workspaceID" />
  <template v-else>
    <PageHeader
      eyebrow="TEAM CALENDAR"
      title="团队日历"
      description="成员可查看完整已确认排班；共同休息仅在所选成员全部明确休息且没有缺失时标记。"
    />
    <div class="toolbar">
      <el-button circle @click="month = month.subtract(1, 'month')"
        ><ArrowLeft :size="16"
      /></el-button>
      <strong>{{ month.format("YYYY年 M月") }}</strong>
      <el-button circle @click="month = month.add(1, 'month')"
        ><ArrowRight :size="16"
      /></el-button>
      <el-select
        v-model="selectedMembers"
        multiple
        collapse-tags
        filterable
        placeholder="选择成员"
      >
        <el-option
          v-for="member in members.data.value?.items ?? []"
          :key="member.id"
          :label="
            member.status === 'ACTIVE'
              ? member.displayName
              : `${member.displayName}（历史成员）`
          "
          :value="member.id"
        />
      </el-select>
      <el-checkbox v-model="onlyAllRest">只看全部休息</el-checkbox>
      <el-checkbox v-model="onlyMissing">只看数据缺失</el-checkbox>
    </div>
    <section class="surface-card card-padding">
      <div class="schedule-grid">
        <button
          v-for="day in days"
          :key="day.date"
          class="calendar-cell"
          :class="{ 'all-rest': day.allRest, missing: day.missing > 0, overdue: day.date < today }"
          @click="open(day)"
        >
          <span class="date">{{ dayjs(day.date).date() }}</span>
          <span class="badge">{{
            day.allRest ? "全部休息" : `工作 ${day.working} · 休息 ${day.rest}`
          }}</span>
          <small
            v-if="day.missing"
            style="display: block; margin-top: 7px; color: var(--warning)"
            >缺失 {{ day.missing }} 人</small
          >
        </button>
      </div>
    </section>
    <el-drawer
      v-model="drawer"
      :title="`${selectedDay?.date ?? ''} · 成员详情`"
      size="min(620px, 96vw)"
    >
      <div class="detail-list">
        <div
          v-for="member in selectedDay?.members ?? []"
          :key="member.userId"
          class="detail-row"
        >
          <span
            ><Users
              :size="15"
              style="vertical-align: middle; margin-right: 8px"
            />{{ memberName(member.userId) }}</span
          >
          <strong style="max-width: 65%; text-align: right">{{
            scheduleDetail(member)
          }}</strong>
        </div>
      </div>
    </el-drawer>
  </template>
</template>
