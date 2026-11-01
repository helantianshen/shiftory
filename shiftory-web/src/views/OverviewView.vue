<script setup lang="ts">
import { computed } from "vue";
import { useQuery } from "@tanstack/vue-query";
import dayjs from "dayjs";
import {
  AlertCircle,
  ArrowUpRight,
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
      title="今天，一目了然"
      :description="`${session.currentWorkspace?.name} · ${dayjs(query.data.value?.date).format('YYYY年MM月DD日')}`"
    >
      <el-button type="primary" @click="$router.push('/import')"
        ><UploadCloud :size="16" />导入排班</el-button
      >
    </PageHeader>
    <div class="stats-grid">
      <article
        v-for="(card, index) in cards"
        :key="card.label"
        class="surface-card stat-card"
      >
        <div class="stat-top">
          <span>{{ card.label }}</span
          ><span
            class="stat-icon"
            :class="['blue', 'green', 'orange', 'purple'][index]"
            ><component :is="card.icon" :size="20"
          /></span>
        </div>
        <div class="stat-number">
          {{ card.value }}<span>{{ index === 3 ? "项" : "人" }}</span>
        </div>
        <div class="stat-footer">
          <span
            class="status-dot"
            :class="['blue', 'green', 'orange', 'purple'][index]"
          ></span
          >{{
            index === 3 ? "当前工作区的待处理任务" : "当前工作区 · 今日排班"
          }}
        </div>
      </article>
    </div>
    <div class="overview-grid">
      <section class="surface-card personal-panel">
        <div class="panel-title">
          <div>
            <h2>我的排班</h2>
            <p>个人排班，全局共享</p>
          </div>
          <CalendarCheck :size="20" class="subtle-icon" />
        </div>
        <div class="completion">
          <el-progress
            type="dashboard"
            :percentage="Math.round(query.data.value?.monthCompleteness ?? 0)"
            :width="184"
            :stroke-width="12"
            color="var(--accent)"
            ><div class="completion-value">
              {{ Math.round(query.data.value?.monthCompleteness ?? 0)
              }}<span>%</span>
            </div>
            <div class="completion-caption">本月完整度</div></el-progress
          >
          <p>每一天的安排，都有迹可循</p>
        </div>
        <div class="next-days">
          <div>
            <span class="detail-icon blue"><Users :size="17" /></span>
            <div>
              <span>下一个工作日</span
              ><strong>{{
                query.data.value?.nextWorkingDate ?? "待安排"
              }}</strong>
            </div>
          </div>
          <div>
            <span class="detail-icon green"><Coffee :size="17" /></span>
            <div>
              <span>下一个休息日</span
              ><strong>{{ query.data.value?.nextRestDate ?? "待安排" }}</strong>
            </div>
          </div>
        </div>
      </section>
      <section class="surface-card team-panel">
        <div class="panel-title">
          <div>
            <h2>团队状态</h2>
            <p>仅统计当前工作区成员</p>
          </div>
          <Users :size="20" class="subtle-icon" />
        </div>
        <div
          v-if="query.data.value?.allRest"
          class="subtle-card common-rest-notice"
        >
          <CalendarCheck :size="18" />今天是全员共同休息日。
        </div>
        <div class="team-summary">
          <div>
            <strong
              >{{
                (query.data.value?.working ?? 0) +
                (query.data.value?.rest ?? 0)
              }}<span> 人</span></strong
            >
            <p>今日已明确排班</p>
          </div>
          <span class="status-pill orange"
            ><AlertCircle :size="13" />{{
              query.data.value?.missing ?? 0
            }}
            人仍需补全</span
          >
        </div>
        <div class="distribution" aria-hidden="true">
          <span
            class="work"
            :style="{ flex: query.data.value?.working ?? 0 }"
          ></span
          ><span
            class="rest"
            :style="{ flex: query.data.value?.rest ?? 0 }"
          ></span
          ><span
            class="missing"
            :style="{ flex: query.data.value?.missing ?? 0 }"
          ></span>
        </div>
        <div class="legend">
          <span
            ><i class="status-dot blue"></i>工作
            {{ query.data.value?.working ?? 0 }} 人</span
          ><span
            ><i class="status-dot green"></i>休息
            {{ query.data.value?.rest ?? 0 }} 人</span
          ><span
            ><i class="status-dot orange"></i>缺失
            {{ query.data.value?.missing ?? 0 }} 人</span
          >
        </div>
        <div class="rest-heading"><h3>近期共同休息</h3></div>
        <div v-if="query.data.value?.allRestDates.length" class="rest-dates">
          <div v-for="date in query.data.value.allRestDates" :key="date">
            <span>{{ dayjs(date).format("YYYY年MM月") }}</span
            ><strong>{{ dayjs(date).format("DD") }}</strong
            ><small>全部休息</small>
          </div>
        </div>
        <p v-else class="muted">暂无共同休息日</p>
        <button class="calendar-link" @click="$router.push('/calendar')">
          打开团队日历<ArrowUpRight :size="17" />
        </button>
      </section>
    </div>
  </template>
</template>
