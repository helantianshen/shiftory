<script setup lang="ts">
import { computed, ref, onMounted, watch } from "vue";
import { useRouter, useRoute } from "vue-router";
import { useQuery } from "@tanstack/vue-query";
import dayjs from "dayjs";
import { ElMessage } from "element-plus";
import { FileSpreadsheet, ImageUp, Text } from "lucide-vue-next";
import { api, formatApiError } from "@/api/client";
import type { ImportJob, Member } from "@/api/types";
import EmptyWorkspace from "@/components/EmptyWorkspace.vue";
import PageHeader from "@/components/PageHeader.vue";
import { useSessionStore } from "@/stores/session";
const session = useSessionStore();
const router = useRouter();
const route = useRoute();
const relatedImportId = Number(route.query.relatedImportId) || undefined;
const workspaceID = computed(() => session.currentWorkspace?.id);
const type = ref<"excel" | "image" | "text">("excel");
const file = ref<File>();
const target = ref(session.user?.id);
const range = ref<[string, string]>([
  dayjs().startOf("month").format("YYYY-MM-DD"),
  dayjs().endOf("month").format("YYYY-MM-DD"),
]);
const instructions = ref("");
const description = ref("");
const requestKey = ref(crypto.randomUUID());
const uploading = ref(false);
const members = useQuery({
  queryKey: computed(() => ["members", workspaceID.value]),
  queryFn: () =>
    api.get<{ items: Member[] }>(`/workspaces/${workspaceID.value}/members`),
  enabled: computed(() => Boolean(workspaceID.value && session.isAdmin)),
});
watch(
  [type, target, range, instructions, description, file],
  () => {
    requestKey.value = crypto.randomUUID();
  },
  { deep: true },
);
onMounted(async () => {
  if (!relatedImportId || !workspaceID.value) return;
  try {
    const old = await api.get<ImportJob>(
      `/workspaces/${workspaceID.value}/imports/${relatedImportId}`,
    );
    type.value = "text";
    target.value = old.targetUserId;
    range.value = [old.periodStart, old.periodEnd];
    description.value = old.description ?? "";
  } catch (error) {
    ElMessage.error(formatApiError(error, "无法读取原描述"));
  }
});

function choose(upload: { raw?: File }) {
  file.value = upload.raw;
}
async function upload() {
  if (
    !workspaceID.value ||
    (type.value !== "text" && !file.value) ||
    (type.value === "text" && !description.value.trim())
  ) {
    ElMessage.warning(type.value === "text" ? "请输入排班描述" : "请选择文件");
    return;
  }
  uploading.value = true;
  try {
    const form = new FormData();
    if (file.value) form.set("file", file.value);
    form.set(
      "targetUserId",
      String(session.isAdmin ? target.value : session.user?.id),
    );
    form.set("periodStart", range.value[0]);
    form.set("periodEnd", range.value[1]);
    if (type.value === "image") form.set("instructions", instructions.value);
    const job =
      type.value === "text"
        ? await api.post<ImportJob>(
            `/workspaces/${workspaceID.value}/imports/text`,
            {
              targetUserId: session.isAdmin ? target.value : session.user?.id,
              periodStart: range.value[0],
              periodEnd: range.value[1],
              description: description.value,
              relatedImportId,
            },
            { "Idempotency-Key": requestKey.value },
          )
        : await api.upload<ImportJob>(
            `/workspaces/${workspaceID.value}/imports${type.value === "image" ? "/image" : ""}`,
            form,
            { "Idempotency-Key": requestKey.value },
          );
    requestKey.value = crypto.randomUUID();
    ElMessage.success(
      type.value !== "excel" ? "识别任务已创建" : "导入预览已生成",
    );
    await router.push({
      path: `/imports/${job.id}`,
      query: { workspaceId: workspaceID.value },
    });
  } catch (error) {
    ElMessage.error(formatApiError(error, "上传失败"));
  } finally {
    uploading.value = false;
  }
}

async function downloadTemplate() {
  const response = await fetch(
    `/api/v1/workspaces/${workspaceID.value}/imports/template.xlsx`,
    {
      credentials: "include",
      headers: { Authorization: `Bearer ${session.accessToken}` },
    },
  );
  if (!response.ok) {
    ElMessage.error("无法下载模板");
    return;
  }
  const blob = await response.blob();
  const objectURL = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = objectURL;
  anchor.download = "shiftory-schedule-template.xlsx";
  anchor.click();
  URL.revokeObjectURL(objectURL);
}
</script>
<template>
  <EmptyWorkspace v-if="!workspaceID" /><template v-else
    ><PageHeader
      eyebrow="IMPORT"
      title="导入排班"
      description="生成排班预览，审核后写入目标成员的全局排班"
      ><el-button @click="downloadTemplate"
        >下载 Excel 模板</el-button
      ></PageHeader
    >
    <div class="grid-3">
      <button
        class="surface-card upload-card"
        :style="type === 'excel' ? 'border-color:var(--accent)' : ''"
        @click="type = 'excel'"
      >
        <FileSpreadsheet :size="28" color="var(--accent)" />
        <h3>上传 Excel 排班表</h3>
        <p class="muted">使用平台模板<br />支持 .xlsx / .xls</p></button
      ><button
        class="surface-card upload-card"
        :style="type === 'image' ? 'border-color:var(--accent)' : ''"
        @click="type = 'image'"
      >
        <ImageUp :size="28" color="var(--accent)" />
        <h3>上传排班截图</h3>
        <p class="muted">AI 识别单人排班<br />GIF 仅读取首帧</p>
      </button>
      <button
        class="surface-card upload-card"
        :style="type === 'text' ? 'border-color:var(--accent)' : ''"
        @click="type = 'text'"
      >
        <Text :size="28" color="var(--accent)" />
        <h3>描述排班规则</h3>
        <p class="muted">描述星期、班次和例外<br />审核后提交</p>
      </button>
    </div>
    <section class="surface-card card-padding" style="margin-top: 18px">
      <el-form label-position="top"
        ><el-form-item v-if="session.isAdmin" label="目标成员"
          ><el-select v-model="target" filterable
            ><el-option
              v-for="member in members.data.value?.items.filter(
                (i) => i.status === 'ACTIVE',
              ) ?? []"
              :key="member.id"
              :label="member.displayName"
              :value="member.id" /></el-select></el-form-item
        ><el-form-item label="排班日期范围"
          ><el-date-picker
            v-model="range"
            type="daterange"
            value-format="YYYY-MM-DD"
            range-separator="至"
            start-placeholder="开始日期"
            end-placeholder="结束日期" /></el-form-item
        ><el-form-item v-if="type === 'image'" label="可选识别说明"
          ><el-input
            v-model="instructions"
            type="textarea"
            :rows="3"
            placeholder="例如：蓝色格表示休息，A 代表早班。"
            maxlength="2000"
            show-word-limit /></el-form-item
        ><el-form-item v-if="type === 'text'" label="排班描述"
          ><el-input
            v-model="description"
            type="textarea"
            :rows="6"
            maxlength="10000"
            show-word-limit
            placeholder="例如：每周一至周五 09:00–18:00 上班，周末休息；10 月 3 日改为 22:00 至次日 06:00。未描述日期会保留为缺失。"
        /></el-form-item>
        <el-form-item
          v-if="type !== 'text'"
          :label="type === 'image' ? '排班截图' : 'Excel 文件'"
          ><el-upload
            drag
            :auto-upload="false"
            :limit="1"
            :accept="
              type === 'image' ? '.png,.jpg,.jpeg,.webp,.gif' : '.xlsx,.xls'
            "
            :on-change="choose"
            :on-remove="() => (file = undefined)"
            ><ImageUp />
            <div>拖拽文件到此处，或点击选择</div></el-upload
          ></el-form-item
        ><el-button type="primary" :loading="uploading" @click="upload">{{
          type !== "excel" ? "创建识别任务" : "生成导入预览"
        }}</el-button></el-form
      >
    </section></template
  >
</template>

<style scoped>
.upload-card {
  min-width: 0;
}

.upload-card .muted {
  line-height: 1.6;
  text-wrap: balance;
}
</style>
