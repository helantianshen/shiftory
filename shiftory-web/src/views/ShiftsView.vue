<script setup lang="ts">
import { computed, reactive, ref } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { ElMessage } from "element-plus";
import { Plus } from "lucide-vue-next";
import { api } from "@/api/client";
import type { Shift } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import { useSessionStore } from "@/stores/session";
const session = useSessionStore();
const workspaceID = computed(() => session.currentWorkspace!.id);
const dialog = ref(false);
const editing = ref<number>();
const form = reactive<{
  name: string;
  code: string;
  startTime?: string;
  endTime?: string;
  crossDay: boolean;
  displayColor: string;
  enabled: boolean;
  sortOrder: number;
  aliases: string;
}>({
  name: "",
  code: "",
  startTime: "08:00",
  endTime: "16:00",
  crossDay: false,
  displayColor: "#319878",
  enabled: true,
  sortOrder: 0,
  aliases: "",
});
const query = useQuery({
  queryKey: computed(() => ["shifts", workspaceID.value]),
  queryFn: () =>
    api.get<{ items: Shift[] }>(`/workspaces/${workspaceID.value}/shifts`),
});
function open(shift?: Shift) {
  editing.value = shift?.id;
  Object.assign(
    form,
    shift
      ? { ...shift, aliases: shift.aliases.join(", ") }
      : {
          name: "",
          code: "",
          startTime: "08:00",
          endTime: "16:00",
          crossDay: false,
          displayColor: "#319878",
          enabled: true,
          sortOrder: query.data.value?.items.length ?? 0,
          aliases: "",
        },
  );
  dialog.value = true;
}
async function save() {
  const payload = {
    ...form,
    startTime: form.startTime || null,
    endTime: form.endTime || null,
    aliases: form.aliases
      .split(/[,，]/)
      .map((i) => i.trim())
      .filter(Boolean),
  };
  if (editing.value)
    await api.put(
      `/workspaces/${workspaceID.value}/shifts/${editing.value}`,
      payload,
    );
  else await api.post(`/workspaces/${workspaceID.value}/shifts`, payload);
  ElMessage.success("班次已保存");
  dialog.value = false;
  await query.refetch();
}
async function toggle(shift: Shift) {
  await api.put(`/workspaces/${workspaceID.value}/shifts/${shift.id}`, {
    ...shift,
    enabled: !shift.enabled,
  });
  await query.refetch();
}
</script>
<template>
  <PageHeader
    eyebrow="SHIFTS"
    title="班次设置"
    description="历史排班保存班次快照，修改定义不会改变过去的数据。"
    ><el-button type="primary" @click="open()"
      ><Plus :size="16" />新增班次</el-button
    ></PageHeader
  >
  <div class="grid-3">
    <article
      v-for="shift in query.data.value?.items ?? []"
      :key="shift.id"
      class="surface-card card-padding"
      :style="{ opacity: shift.enabled ? 1 : 0.55 }"
    >
      <div
        style="
          display: flex;
          justify-content: space-between;
          align-items: start;
        "
      >
        <span
          class="stat-icon"
          :style="{ background: shift.displayColor, color: '#fff' }"
          >{{ shift.code.slice(0, 1) }}</span
        ><el-switch :model-value="shift.enabled" @change="toggle(shift)" />
      </div>
      <h3>
        {{ shift.name }} <small class="muted">{{ shift.code }}</small>
      </h3>
      <p>
        {{
          shift.startTime && shift.endTime
            ? `${shift.startTime} – ${shift.endTime}${shift.crossDay ? "（次日）" : ""}`
            : "未设置固定时间"
        }}
      </p>
      <p class="muted">别名：{{ shift.aliases.join("、") || "无" }}</p>
      <el-button text type="primary" @click="open(shift)">编辑班次</el-button>
    </article>
  </div>
  <el-dialog
    v-model="dialog"
    :title="editing ? '编辑班次' : '新增班次'"
    width="min(580px,94vw)"
    ><el-form label-position="top"
      ><div class="grid-2">
        <el-form-item label="名称"
          ><el-input v-model="form.name" /></el-form-item
        ><el-form-item label="代码"
          ><el-input v-model="form.code" /></el-form-item
        ><el-form-item label="开始时间"
          ><el-time-select
            v-model="form.startTime"
            start="00:00"
            step="00:15"
            end="23:45"
            clearable /></el-form-item
        ><el-form-item label="结束时间"
          ><el-time-select
            v-model="form.endTime"
            start="00:00"
            step="00:15"
            end="23:45"
            clearable
        /></el-form-item>
      </div>
      <el-form-item
        ><el-checkbox v-model="form.crossDay"
          >结束时间属于次日</el-checkbox
        ></el-form-item
      ><el-form-item label="展示颜色"
        ><el-color-picker v-model="form.displayColor" /></el-form-item
      ><el-form-item label="识别别名"
        ><el-input
          v-model="form.aliases"
          placeholder="早、A、Morning（逗号分隔）" /></el-form-item></el-form
    ><template #footer
      ><el-button @click="dialog = false">取消</el-button
      ><el-button type="primary" @click="save">保存</el-button></template
    ></el-dialog
  >
</template>
