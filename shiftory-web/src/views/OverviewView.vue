<script setup lang="ts">
import { computed } from "vue";
import { useQuery } from "@tanstack/vue-query";
import dayjs from "dayjs";
import {
  AlertCircle,
  CalendarCheck,
  Coffee,
  UploadCloud,
  Users,
} from "lucide-vue-next";
import { api } from "@/api/client";
import EmptyWorkspace from "@/components/EmptyWorkspace.vue";
import PageHeader from "@/components/PageHeader.vue";
import { useSessionStore } from "@/stores/session";

const session = useSessionStore();
const workspaceID = computed(() => session.currentWorkspace?.id);
interface Overview {
  date: string;
  working: number;
  rest: number;
  missing: number;
  allRest: boolean;
  pendingImports: number;
  monthCompleteness: number;
  nextWorkingDate?: string | null;
  nextRestDate?: string | null;
  allRestDates: string[];
}
const query = useQuery({
  queryKey: computed(() => ["overview", workspaceID.value]),
  queryFn: () => api.get<Overview>(`/workspaces/${workspaceID.value}/overview`),
  enabled: computed(() => Boolean(workspaceID.value)),
});
const cards = computed(() => [
  { label: "今日工作", value: query.data.value?.working ?? "—", icon: Users },
  { label: "今日休息", value: query.data.value?.rest ?? "—", icon: Coffee },
  {
    label: "排班缺失",
    value: query.data.value?.missing ?? "—",
    icon: AlertCircle,
  },
  {
    label: "待确认导入",
    value: query.data.value?.pendingImports ?? "—",
    icon: UploadCloud,
  },
]);
</script>

<template>
  <EmptyWorkspace v-if="!workspaceID" />
  <template v-else>
    <PageHeader
      eyebrow="OVERVIEW"
      title="今天，一目了然"
      :description="`${session.currentWorkspace?.name} · ${dayjs(query.data.value?.date).format('YYYY年MM月DD日')}`"
    >
      <el-button type="primary" @click="$router.push('/import')"
        ><UploadCloud :size="16" />导入排班</el-button
      >
    </PageHeader>
    <div class="grid-4">
      <article
        v-for="card in cards"
        :key="card.label"
        class="surface-card stat-card"
      >
        <span class="stat-icon"><component :is="card.icon" :size="18" /></span>
        <div class="value">{{ card.value }}</div>
        <div class="label">{{ card.label }}</div>
      </article>
    </div>
    <div class="grid-2" style="margin-top: 18px">
      <section class="surface-card card-padding">
        <h2 class="section-title">我的排班</h2>
        <el-progress
          type="dashboard"
          :percentage="Math.round(query.data.value?.monthCompleteness ?? 0)"
        />
        <div class="detail-list">
          <div class="detail-row">
            <span>下一个工作日</span
            ><strong>{{
              query.data.value?.nextWorkingDate ?? "待安排"
            }}</strong>
          </div>
          <div class="detail-row">
            <span>下一个休息日</span
            ><strong>{{ query.data.value?.nextRestDate ?? "待安排" }}</strong>
          </div>
        </div>
      </section>
      <section class="surface-card card-padding">
        <h2 class="section-title">团队状态</h2>
        <div v-if="query.data.value?.allRest" class="subtle-card">
          <CalendarCheck
            :size="18"
            style="vertical-align: middle; margin-right: 8px"
          />今天是全员共同休息日。
        </div>
        <div class="detail-list">
          <div class="detail-row">
            <span>已明确排班</span
            ><strong
              >{{
                (query.data.value?.working ?? 0) + (query.data.value?.rest ?? 0)
              }}
              人</strong
            >
          </div>
          <div class="detail-row">
            <span>仍需补全</span
            ><strong>{{ query.data.value?.missing ?? 0 }} 人</strong>
          </div>
          <div class="detail-row">
            <span>近期共同休息</span
            ><strong>{{
              query.data.value?.allRestDates.join("、") || "暂无"
            }}</strong>
          </div>
        </div>
        <el-button text type="primary" @click="$router.push('/calendar')"
          >打开团队日历 →</el-button
        >
      </section>
    </div>
  </template>
</template>
