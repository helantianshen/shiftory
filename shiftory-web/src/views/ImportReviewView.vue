<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useQuery } from "@tanstack/vue-query";
import { ElMessage, ElMessageBox } from "element-plus";
import { invalidateScheduleViews } from "@/api/query-client";
import { api, formatApiError } from "@/api/client";
import type { ImportItem, ImportJob, ScheduleDay, Shift } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import ScheduleEditor from "@/components/ScheduleEditor.vue";
import { useSessionStore } from "@/stores/session";
import {
  IMPORT_CATEGORY_GUIDE,
  IMPORT_COMMIT_CONFIRM_MESSAGE,
  IMPORT_COMMIT_CONFIRM_OPTIONS,
  importCategoryLabel,
  importStateLabel,
} from "./import-review-content";

const session = useSessionStore();
const route = useRoute();
const router = useRouter();
const readOnly = computed(() => Boolean(route.meta.readOnly));

const jobID = Number(route.params.id);
const workspaceID = computed(() => session.currentWorkspace?.id ?? 0);
const stageLabels: Record<string, string> = {
  WAITING: "等待调度",
  WAITING_PROVIDER: "等待供应商恢复",
  RECOGNIZING: "正在识别",
  REVIEW: "等待审核",
  PAUSED: "AI 已停用，任务暂停",
  PENDING: "等待重试",
  PARSING: "正在处理",
  FAILED: "识别失败",
  COMPLETED: "已完成",
  CANCELLED: "已取消",
  ROLLED_BACK: "已撤销",
};
const saving = ref(false);
const decisionsDirty = ref(false);
const baseReviewVersion = ref<number>();
const rawResponse = ref<string>();
const calls = ref<
  {
    attemptId: number;
    id: number;
    providerId: string;
    model: string;
    code: string;
  }[]
>([]);
const editing = ref<ImportItem>();
const query = useQuery({
  enabled: computed(() => workspaceID.value > 0),
  queryKey: computed(() => ["import", workspaceID.value, jobID]),
  queryFn: () =>
    api.get<ImportJob>(`/workspaces/${workspaceID.value}/imports/${jobID}`),
  refetchInterval: (state) =>
    ["PENDING", "PARSING"].includes(state.state.data?.state ?? "")
      ? 2500
      : false,
});
const shifts = useQuery({
  enabled: computed(() => workspaceID.value > 0),
  queryKey: computed(() => ["shifts", workspaceID.value]),
  queryFn: () =>
    api.get<{ items: Shift[] }>(`/workspaces/${workspaceID.value}/shifts`),
});
const job = computed(() => query.data.value);
watch(job, (value) => {
  if (!editing.value && !decisionsDirty.value)
    baseReviewVersion.value = value?.reviewVersion;
});
const reviewVersion = () => baseReviewVersion.value ?? job.value?.reviewVersion;
async function retry() {
  try {
    await api.post(
      `/workspaces/${workspaceID.value}/imports/${jobID}/retry`,
      {},
    );
    await query.refetch();
  } catch (error) {
    ElMessage.error(formatApiError(error));
  }
}
async function refresh() {
  if (decisionsDirty.value || editing.value) {
    ElMessage.warning("请先保存当前修改");
    return;
  }
  try {
    await api.post(
      `/workspaces/${workspaceID.value}/imports/${jobID}/refresh-preview`,
      { expectedReviewVersion: reviewVersion() },
    );
    await query.refetch();
  } catch (error) {
    ElMessage.error(formatApiError(error));
  }
}
async function loadCalls() {
  try {
    const attempts = await api.get<{ items: { id: number }[] }>(
      `/workspaces/${workspaceID.value}/imports/${jobID}/attempts`,
    );
    const results = await Promise.all(
      attempts.items.map(async (a) => {
        const r = await api.get<{
          items: {
            id: number;
            providerId: string;
            model: string;
            code: string;
          }[];
        }>(
          `/workspaces/${workspaceID.value}/imports/${jobID}/attempts/${a.id}/calls`,
        );
        return r.items.map((c) => ({ ...c, attemptId: a.id }));
      }),
    );
    calls.value = results.flat();
  } catch (error) {
    ElMessage.error(formatApiError(error));
  }
}

const canManage = computed(
  () =>
    !readOnly.value &&
    Boolean(job.value) &&
    (session.isAdmin || job.value?.targetUserId === session.user?.id),
);
const canReview = computed(
  () => canManage.value && job.value?.state === "NEEDS_REVIEW",
);
const conflicts = computed(
  () => job.value?.items?.filter((item) => item.type === "CONFLICT") ?? [],
);
const unresolved = computed(
  () => conflicts.value.filter((item) => !item.decision).length,
);
async function viewRaw(call: { attemptId: number; id: number }) {
  try {
    const r = await api.get<{ raw: string }>(
      `/workspaces/${workspaceID.value}/imports/${jobID}/attempts/${call.attemptId}/calls/${call.id}/raw`,
    );
    rawResponse.value = r.raw;
  } catch (error) {
    ElMessage.error(formatApiError(error));
  }
}

const ruleLabels: Record<string, string> = {
  WEEKLY: "每周",
  DATE_RANGE: "连续日期",
  DATE: "单日例外",
  CYCLE: "周期轮班",
};
function ruleSummary(rule: {
  type: string;
  date?: string;
  start?: string;
  end?: string;
  weekdays?: number[];
  anchorDate?: string;
  cycleDays?: number;
  dayOffsets?: number[];
  status: string;
  segments: {
    type: string;
    mappedShiftCode?: string;
    startTime?: string;
    endTime?: string;
    crossDay: boolean;
  }[];
}) {
  const condition =
    rule.type === "WEEKLY"
      ? `星期 ${rule.weekdays?.join("、")}`
      : rule.type === "DATE"
        ? rule.date
        : rule.type === "CYCLE"
          ? `从 ${rule.anchorDate} 起每 ${rule.cycleDays} 天，偏移 ${rule.dayOffsets?.join("、")}`
          : `${rule.start} 至 ${rule.end}`;
  const shifts = rule.segments
    .map((s) =>
      s.type === "SHIFT"
        ? s.mappedShiftCode
        : `${s.startTime}–${s.endTime}${s.crossDay ? "（次日）" : ""}`,
    )
    .join("、");
  return `${ruleLabels[rule.type]} ${condition}：${rule.status === "REST" ? "休息" : "工作"} ${shifts}`;
}
const editorModel = computed<ScheduleDay | null>(() => {
  if (!editing.value?.draft) return null;
  return {
    id: 0,
    workspaceId: workspaceID.value,
    userId: job.value?.targetUserId ?? session.user!.id,
    workDate: editing.value.workDate,
    status: editing.value.draft.status as "WORKING" | "REST",
    sourceType: job.value?.importType ?? "IMAGE_AI",
    note: editing.value.draft.note ?? "",
    version: 0,
    segments: editing.value.draft.segments ?? [],
  };
});

function setAll(decision: "KEEP_EXISTING" | "USE_IMPORTED" | "SKIP") {
  if (!canReview.value) return;
  decisionsDirty.value = true;
  for (const item of conflicts.value) item.decision = decision;
}
async function saveDecisions() {
  try {
    if (!canReview.value) return;
    const decisions = (job.value?.items ?? [])
      .filter((item) => item.decision)
      .map((item) => ({ itemId: item.id, decision: item.decision }));
    if (!decisions.length) return;
    const response = await api.put<{ reviewVersion: number }>(
      `/workspaces/${workspaceID.value}/imports/${jobID}/decisions`,
      {
        decisions,
        expectedReviewVersion: reviewVersion(),
      },
    );
    baseReviewVersion.value = response.reviewVersion;
    decisionsDirty.value = false;
    ElMessage.success("冲突决策已保存");
    await query.refetch();
  } catch (error) {
    ElMessage.error(formatApiError(error));
    throw error;
  }
}
async function saveCorrection(payload: {
  status: "WORKING" | "REST";
  note: string;
  version: number;
  segments: unknown[];
}) {
  try {
    if (!canReview.value || !editing.value) return;
    if (decisionsDirty.value) await saveDecisions();
    const response = await api.put<{ reviewVersion: number }>(
      `/workspaces/${workspaceID.value}/imports/${jobID}/items/${editing.value.id}`,
      {
        status: payload.status,
        note: payload.note,
        segments: payload.segments,
        expectedReviewVersion: reviewVersion(),
      },
    );
    baseReviewVersion.value = response.reviewVersion;
    editing.value = undefined;
    ElMessage.success("预览项已按人工核对结果修正");
    await query.refetch();
  } catch (error) {
    ElMessage.error(formatApiError(error));
    throw error;
  }
}
async function commit() {
  if (!canReview.value) return;
  if (unresolved.value) {
    ElMessage.warning("请先处理全部冲突");
    return;
  }
  await ElMessageBox.confirm(
    IMPORT_COMMIT_CONFIRM_MESSAGE,
    "确认导入",
    IMPORT_COMMIT_CONFIRM_OPTIONS,
  );
  saving.value = true;
  try {
    await saveDecisions();
    await api.post(`/workspaces/${workspaceID.value}/imports/${jobID}/commit`, {
      expectedReviewVersion: reviewVersion(),
    });
    ElMessage.success("导入已提交");
    await invalidateScheduleViews();
  } catch (error) {
    ElMessage.error(formatApiError(error));
  } finally {
    saving.value = false;
  }
}
async function rollback() {
  if (!canManage.value) return;
  try {
    await ElMessageBox.confirm(
      "撤销会恢复导入前的数据；若排班已被再次修改，整次撤销将被拒绝。",
      "撤销导入",
      { type: "warning" },
    );
    await api.post(
      `/workspaces/${workspaceID.value}/imports/${jobID}/rollback`,
      {},
    );
    ElMessage.success("导入已撤销");
    await invalidateScheduleViews();
  } catch (error) {
    if (error !== "cancel" && error !== "close")
      ElMessage.error(formatApiError(error));
  }
}
async function cancel() {
  if (!canManage.value) return;
  try {
    await api.post(
      `/workspaces/${workspaceID.value}/imports/${jobID}/cancel`,
      {},
    );
    ElMessage.success("任务已取消");
    await query.refetch();
  } catch (error) {
    ElMessage.error(formatApiError(error));
  }
}
async function download() {
  const response = await fetch(
    `/api/v1/workspaces/${workspaceID.value}/imports/${jobID}/file`,
    {
      credentials: "include",
      headers: { Authorization: `Bearer ${session.accessToken}` },
    },
  );
  if (!response.ok) {
    ElMessage.error("无法下载原文件");
    return;
  }
  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = job.value?.sourceFilename ?? "import-file";
  anchor.click();
  URL.revokeObjectURL(url);
}
function draftLabel(item: ImportItem) {
  if (!item.draft) return "—";
  if (item.draft.status === "REST") return "休息";
  return (
    item.draft.segments
      ?.map((segment) => {
        const label =
          segment.shiftName ??
          (segment.startTime
            ? `${segment.startTime}–${segment.endTime}`
            : "工作");
        return label + (segment.crossDay ? "（次日）" : "");
      })
      .join("；") || "工作"
  );
}
function issueLabel(item: ImportItem) {
  const issues = item.issues?.map((issue) =>
    typeof issue === "string"
      ? issue
      : [issue.field, issue.message].filter(Boolean).join("："),
  );
  return (
    [...(issues ?? []), ...(item.errorMessage ? [item.errorMessage] : [])].join(
      "；",
    ) || "—"
  );
}
</script>

<template>
  <PageHeader
    eyebrow="IMPORT REVIEW"
    :title="`导入任务 #${jobID}`"
    :description="
      job
        ? `${job.sourceFilename} · ${job.periodStart} 至 ${job.periodEnd}`
        : '正在读取…'
    "
  >
    <el-button v-if="job?.importType !== 'TEXT_AI'" @click="download"
      >下载原文件</el-button
    >
    <el-button
      v-if="
        canManage &&
        job?.state === 'FAILED' &&
        ['TEXT_AI', 'IMAGE_AI'].includes(job.importType)
      "
      @click="retry"
      >重新识别</el-button
    >
    <el-button
      v-if="canManage && job?.importType === 'TEXT_AI'"
      @click="
        router.push({
          path: '/import',
          query: { workspaceId: workspaceID, relatedImportId: jobID },
        })
      "
      >修改描述并重新生成</el-button
    >
    <el-button v-if="canReview" @click="refresh">刷新排班基线</el-button>
    <el-button
      v-if="job && ['TEXT_AI', 'IMAGE_AI'].includes(job.importType)"
      @click="loadCalls"
      >查看供应商调用</el-button
    >
    <el-button
      v-if="
        canManage &&
        ['PENDING', 'PARSING', 'NEEDS_REVIEW'].includes(job?.state ?? '')
      "
      @click="cancel"
      >取消任务</el-button
    >
    <el-button
      v-if="canManage && job?.state === 'COMPLETED'"
      type="danger"
      plain
      @click="rollback"
      >撤销导入</el-button
    >
  </PageHeader>
  <section
    v-if="job"
    class="surface-card card-padding"
    style="margin-bottom: 18px"
  >
    <p v-if="job.description" style="white-space: pre-wrap">
      {{ job.description }}
    </p>
    <p v-if="job.stage">
      当前阶段：{{ stageLabels[job.stage] ?? "正在处理" }} · 识别轮次：{{
        job.round ?? 0
      }}
    </p>
    <el-alert
      v-if="job.errorMessage"
      :title="job.errorMessage"
      type="warning"
      :closable="false"
    />
    <p v-if="job.nextRetryAt">下次重试：{{ job.nextRetryAt }}</p>
    <p v-for="issue in job.issues" :key="issue.field + issue.message">
      {{ issue.message }}
    </p>
    <details v-if="job.rules">
      <summary>解析规则与例外</summary>
      <p v-for="(rule, index) in job.rules.rules" :key="index">
        {{ ruleSummary(rule) }}
      </p>
    </details>
    <p v-for="call in calls" :key="call.id">
      {{ call.providerId }} · {{ call.model }} · {{ call.code }}
      <el-button text @click="viewRaw(call)">查看模型原文</el-button>
    </p>
    <p v-if="job.state === 'COMPLETED'">
      实际写入 {{ job.writeCount ?? 0 }} 天排班
    </p>
    <div class="grid-4">
      <div>
        <span class="muted">状态</span>
        <h3>{{ importStateLabel(job.state) }}</h3>
      </div>
      <div>
        <span class="muted">条目</span>
        <h3>{{ job.itemCount }}</h3>
      </div>
      <div>
        <span class="muted">冲突</span>
        <h3>{{ job.conflictCount }}</h3>
      </div>
      <div>
        <span class="muted">不确定 / 无效</span>
        <h3>{{ job.invalidCount }}</h3>
      </div>
    </div>
    <div class="category-guide">
      <span class="category-guide__title">分类说明</span>
      <div class="category-guide__items">
        <span
          v-for="item in IMPORT_CATEGORY_GUIDE"
          :key="item.type"
          class="category-guide__item"
        >
          <strong>{{ importCategoryLabel(item.type) }}</strong>
          {{ item.description }}
        </span>
      </div>
    </div>
  </section>
  <section
    v-if="job?.state === 'PENDING' || job?.state === 'PARSING'"
    class="surface-card card-padding"
  >
    <el-progress
      :percentage="job.state === 'PARSING' ? 62 : 18"
      :indeterminate="job.state === 'PARSING'"
    />
    <p class="muted">图片识别由数据库 Worker 异步执行，本页会自动刷新。</p>
  </section>
  <section v-else-if="job?.items" class="surface-card table-card">
    <div
      v-if="canReview && conflicts.length"
      class="toolbar card-padding"
      style="margin: 0"
    >
      <strong>冲突批量决策</strong>
      <el-button size="small" @click="setAll('KEEP_EXISTING')"
        >全部保留原排班</el-button
      >
      <el-button size="small" @click="setAll('USE_IMPORTED')"
        >全部使用导入</el-button
      >
      <el-button size="small" @click="setAll('SKIP')">全部跳过</el-button>
      <span class="muted">未处理 {{ unresolved }} 项</span>
    </div>
    <el-table :data="job.items">
      <el-table-column prop="workDate" label="日期" width="125" />
      <el-table-column label="分类" width="110">
        <template #default="scope">{{
          importCategoryLabel(scope.row.type)
        }}</template>
      </el-table-column>
      <el-table-column label="导入草稿" min-width="180">
        <template #default="scope">{{ draftLabel(scope.row) }}</template>
      </el-table-column>
      <el-table-column
        v-if="job.importType === 'TEXT_AI'"
        label="来源规则"
        min-width="150"
      >
        <template #default="scope">{{
          scope.row.draft?.ruleIds?.join("、") || "—"
        }}</template>
      </el-table-column>
      <el-table-column label="识别问题" min-width="220">
        <template #default="scope">{{ issueLabel(scope.row) }}</template>
      </el-table-column>
      <el-table-column label="决策" min-width="390">
        <template #default="scope">
          <el-radio-group
            v-if="scope.row.type === 'CONFLICT'"
            v-model="scope.row.decision"
            @change="decisionsDirty = true"
            :disabled="!canReview"
          >
            <el-radio-button value="KEEP_EXISTING">保留原排班</el-radio-button>
            <el-radio-button value="USE_IMPORTED">使用导入</el-radio-button>
            <el-radio-button value="SKIP">跳过</el-radio-button>
          </el-radio-group>
          <el-select
            v-else-if="canReview && scope.row.type !== 'SAME'"
            v-model="scope.row.decision"
            placeholder="默认选择"
            @change="decisionsDirty = true"
            ><el-option label="跳过" value="SKIP" /><el-option
              v-if="scope.row.type === 'NEW'"
              label="导入"
              value="USE_IMPORTED"
          /></el-select>
          <span v-else class="muted">{{
            scope.row.type === "NEW"
              ? "默认导入"
              : scope.row.type === "SAME"
                ? "无需修改"
                : "不写入"
          }}</span>
          <el-button
            v-if="canReview"
            text
            type="primary"
            @click="editing = scope.row"
            >人工修正</el-button
          >
        </template>
      </el-table-column>
    </el-table>
    <div
      v-if="canReview"
      class="card-padding"
      style="display: flex; justify-content: flex-end; gap: 10px"
    >
      <el-button @click="saveDecisions">保存决策</el-button>
      <el-button type="primary" :loading="saving" @click="commit"
        >确认并提交导入</el-button
      >
    </div>
  </section>
  <el-empty v-else description="暂无预览数据">
    <el-button @click="router.push('/imports')">返回导入记录</el-button>
  </el-empty>
  <el-dialog
    :model-value="rawResponse !== undefined"
    title="模型原始输出"
    @close="rawResponse = undefined"
  >
    <pre style="white-space: pre-wrap; overflow-wrap: anywhere">{{
      rawResponse
    }}</pre>
  </el-dialog>
  <el-dialog
    :model-value="Boolean(editing)"
    title="人工核对导入项"
    width="min(720px, 96vw)"
    @close="editing = undefined"
  >
    <ScheduleEditor
      v-if="editing"
      :date="editing.workDate"
      :shifts="shifts.data.value?.items ?? []"
      :model-value="editorModel"
      @save="saveCorrection"
    />
  </el-dialog>
</template>

<style scoped>
.category-guide {
  margin-top: 16px;
  padding-top: 14px;
  border-top: 1px solid var(--line);
}

.category-guide__title {
  display: block;
  margin-bottom: 10px;
  color: var(--ink);
  font-size: 13px;
  font-weight: 700;
}

.category-guide__items {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px 18px;
}

.category-guide__item {
  color: var(--muted);
  font-size: 13px;
  line-height: 1.6;
}

.category-guide__item strong {
  margin-right: 6px;
  color: var(--ink);
}

@media (max-width: 900px) {
  .category-guide__items {
    grid-template-columns: 1fr;
  }
}
</style>
