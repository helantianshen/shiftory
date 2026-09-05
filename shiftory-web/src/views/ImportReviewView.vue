<script setup lang="ts">
import { computed, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useQuery, useQueryClient } from "@tanstack/vue-query";
import { ElMessage, ElMessageBox } from "element-plus";
import { api } from "@/api/client";
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
const queryClient = useQueryClient();
const jobID = Number(route.params.id);
const workspaceID = computed(() => session.currentWorkspace!.id);
const saving = ref(false);
const editing = ref<ImportItem>();
const query = useQuery({
  queryKey: computed(() => ["import", workspaceID.value, jobID]),
  queryFn: () =>
    api.get<ImportJob>(`/workspaces/${workspaceID.value}/imports/${jobID}`),
  refetchInterval: (state) =>
    ["PENDING", "PARSING"].includes(state.state.data?.state ?? "")
      ? 2500
      : false,
});
const shifts = useQuery({
  queryKey: computed(() => ["shifts", workspaceID.value]),
  queryFn: () =>
    api.get<{ items: Shift[] }>(`/workspaces/${workspaceID.value}/shifts`),
});
const job = computed(() => query.data.value);
const conflicts = computed(
  () => job.value?.items?.filter((item) => item.type === "CONFLICT") ?? [],
);
const unresolved = computed(
  () => conflicts.value.filter((item) => !item.decision).length,
);
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
  for (const item of conflicts.value) item.decision = decision;
}
async function saveDecisions() {
  const decisions = conflicts.value
    .filter((item) => item.decision)
    .map((item) => ({ itemId: item.id, decision: item.decision }));
  if (!decisions.length) return;
  await api.put(`/workspaces/${workspaceID.value}/imports/${jobID}/decisions`, {
    decisions,
  });
  ElMessage.success("冲突决策已保存");
  await query.refetch();
}
async function saveCorrection(payload: {
  status: "WORKING" | "REST";
  note: string;
  version: number;
  segments: unknown[];
}) {
  if (!editing.value) return;
  await api.put(
    `/workspaces/${workspaceID.value}/imports/${jobID}/items/${editing.value.id}`,
    { status: payload.status, note: payload.note, segments: payload.segments },
  );
  editing.value = undefined;
  ElMessage.success("预览项已按人工核对结果修正");
  await query.refetch();
}
async function commit() {
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
    await api.post(
      `/workspaces/${workspaceID.value}/imports/${jobID}/commit`,
      {},
    );
    ElMessage.success("导入已提交");
    await queryClient.invalidateQueries({
      queryKey: ["import", workspaceID.value, jobID],
    });
  } finally {
    saving.value = false;
  }
}
async function rollback() {
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
  await query.refetch();
}
async function cancel() {
  await api.post(
    `/workspaces/${workspaceID.value}/imports/${jobID}/cancel`,
    {},
  );
  ElMessage.success("任务已取消");
  await query.refetch();
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
  const first = item.draft.segments?.[0];
  return item.draft.status === "REST"
    ? "休息"
    : (first?.shiftName ??
        (first?.startTime ? `${first.startTime}–${first.endTime}` : "工作"));
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
    <el-button @click="download">下载原文件</el-button>
    <el-button
      v-if="['PENDING', 'PARSING', 'NEEDS_REVIEW'].includes(job?.state ?? '')"
      @click="cancel"
      >取消任务</el-button
    >
    <el-button
      v-if="job?.state === 'COMPLETED'"
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
    <div v-if="conflicts.length" class="toolbar card-padding" style="margin: 0">
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
        <template #default="scope">{{ importCategoryLabel(scope.row.type) }}</template>
      </el-table-column>
      <el-table-column label="导入草稿" min-width="180">
        <template #default="scope">{{ draftLabel(scope.row) }}</template>
      </el-table-column>
      <el-table-column label="识别问题" min-width="220">
        <template #default="scope">{{ issueLabel(scope.row) }}</template>
      </el-table-column>
      <el-table-column label="决策" min-width="390">
        <template #default="scope">
          <el-radio-group
            v-if="scope.row.type === 'CONFLICT'"
            v-model="scope.row.decision"
          >
            <el-radio-button value="KEEP_EXISTING">保留原排班</el-radio-button>
            <el-radio-button value="USE_IMPORTED">使用导入</el-radio-button>
            <el-radio-button value="SKIP">跳过</el-radio-button>
          </el-radio-group>
          <span v-else class="muted">{{
            scope.row.type === "NEW"
              ? "默认导入"
              : scope.row.type === "SAME"
                ? "无需修改"
                : "不写入"
          }}</span>
          <el-button
            v-if="job.state === 'NEEDS_REVIEW' && scope.row.type !== 'SAME'"
            text
            type="primary"
            @click="editing = scope.row"
            >人工修正</el-button
          >
        </template>
      </el-table-column>
    </el-table>
    <div
      v-if="job.state === 'NEEDS_REVIEW'"
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
