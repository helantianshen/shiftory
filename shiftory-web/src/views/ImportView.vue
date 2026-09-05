<script setup lang="ts">
import { computed, ref } from "vue";
import { useRouter } from "vue-router";
import { useQuery } from "@tanstack/vue-query";
import dayjs from "dayjs";
import { ElMessage } from "element-plus";
import { FileSpreadsheet, ImageUp } from "lucide-vue-next";
import { api, formatApiError } from "@/api/client";
import type { ImportJob, Member } from "@/api/types";
import EmptyWorkspace from "@/components/EmptyWorkspace.vue";
import PageHeader from "@/components/PageHeader.vue";
import { useSessionStore } from "@/stores/session";
const session = useSessionStore();
const router = useRouter();
const workspaceID = computed(() => session.currentWorkspace?.id);
const type = ref<"excel" | "image">("excel");
const file = ref<File>();
const target = ref(session.user?.id);
const range = ref<[string, string]>([
  dayjs().startOf("month").format("YYYY-MM-DD"),
  dayjs().endOf("month").format("YYYY-MM-DD"),
]);
const instructions = ref("");
const uploading = ref(false);
const members = useQuery({
  queryKey: computed(() => ["members", workspaceID.value]),
  queryFn: () =>
    api.get<{ items: Member[] }>(`/workspaces/${workspaceID.value}/members`),
  enabled: computed(() => Boolean(workspaceID.value && session.isAdmin)),
});
function choose(upload: { raw?: File }) {
  file.value = upload.raw;
}
async function upload() {
  if (!file.value || !workspaceID.value) {
    ElMessage.warning("请选择文件");
    return;
  }
  uploading.value = true;
  try {
    const form = new FormData();
    form.set("file", file.value);
    form.set(
      "targetUserId",
      String(session.isAdmin ? target.value : session.user?.id),
    );
    form.set("periodStart", range.value[0]);
    form.set("periodEnd", range.value[1]);
    if (type.value === "image") form.set("instructions", instructions.value);
    const job = await api.upload<ImportJob>(
      `/workspaces/${workspaceID.value}/imports${type.value === "image" ? "/image" : ""}`,
      form,
    );
    ElMessage.success(
      type.value === "image" ? "识别任务已创建" : "导入预览已生成",
    );
    await router.push(`/imports/${job.id}`);
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
      description="Excel 与截图最终都进入同一套预览和人工确认流程。"
      ><el-button @click="downloadTemplate"
        >下载 Excel 模板</el-button
      ></PageHeader
    >
    <div class="grid-2">
      <button
        class="surface-card upload-card"
        :style="type === 'excel' ? 'border-color:var(--accent)' : ''"
        @click="type = 'excel'"
      >
        <FileSpreadsheet :size="28" color="var(--accent)" />
        <h3>上传 Excel 排班表</h3>
        <p class="muted">支持平台固定模板的 .xlsx 与 .xls 文件。</p></button
      ><button
        class="surface-card upload-card"
        :style="type === 'image' ? 'border-color:var(--accent)' : ''"
        @click="type = 'image'"
      >
        <ImageUp :size="28" color="var(--accent)" />
        <h3>上传排班截图</h3>
        <p class="muted">由 AI 异步识别，一张图只对应一名成员。</p>
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
        ><el-form-item :label="type === 'image' ? '排班截图' : 'Excel 文件'"
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
          type === "image" ? "创建识别任务" : "生成导入预览"
        }}</el-button></el-form
      >
    </section></template
  >
</template>
