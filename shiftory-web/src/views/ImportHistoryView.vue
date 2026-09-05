<script setup lang="ts">
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useQuery } from "@tanstack/vue-query";
import dayjs from "dayjs";
import { api } from "@/api/client";
import type { ImportJob } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import EmptyWorkspace from "@/components/EmptyWorkspace.vue";
import { useSessionStore } from "@/stores/session";
const session = useSessionStore();
const route = useRoute();
const router = useRouter();
const workspaceID = computed(() => session.currentWorkspace?.id);
const query = useQuery({
  queryKey: computed(() => [
    "imports",
    workspaceID.value,
    route.meta.allImports,
  ]),
  queryFn: () =>
    api.get<{ items: ImportJob[] }>(`/workspaces/${workspaceID.value}/imports`),
  enabled: computed(() => Boolean(workspaceID.value)),
  refetchInterval: 10000,
});
const title = computed(() =>
  route.meta.allImports ? "全部导入记录" : "我的导入记录",
);
function stateLabel(state: string) {
  return (
    (
      {
        PENDING: "等待处理",
        PARSING: "识别中",
        NEEDS_REVIEW: "等待确认",
        COMPLETED: "已完成",
        FAILED: "失败",
        CANCELLED: "已取消",
        ROLLED_BACK: "已撤销",
      } as Record<string, string>
    )[state] ?? state
  );
}
</script>
<template>
  <EmptyWorkspace v-if="!workspaceID" /><template v-else
    ><PageHeader
      eyebrow="IMPORT HISTORY"
      :title="title"
      description="查看解析进度、冲突数量、原文件和安全撤销状态。"
      ><el-button type="primary" @click="router.push('/import')"
        >新建导入</el-button
      ></PageHeader
    >
    <section class="surface-card table-card">
      <el-table
        :data="query.data.value?.items ?? []"
        @row-click="(row: ImportJob) => router.push(`/imports/${row.id}`)"
        ><el-table-column
          prop="sourceFilename"
          label="文件"
          min-width="190"
        /><el-table-column label="类型" width="100"
          ><template #default="scope"
            ><span class="type-pill">{{ scope.row.importType }}</span></template
          ></el-table-column
        ><el-table-column
          prop="targetUserId"
          label="目标成员"
          width="110"
        /><el-table-column label="周期" min-width="190"
          ><template #default="scope"
            >{{ scope.row.periodStart }} 至 {{ scope.row.periodEnd }}</template
          ></el-table-column
        ><el-table-column label="状态" width="110"
          ><template #default="scope"
            ><span
              class="state-pill"
              :class="{ failed: scope.row.state === 'FAILED' }"
              >{{ stateLabel(scope.row.state) }}</span
            ></template
          ></el-table-column
        ><el-table-column
          prop="conflictCount"
          label="冲突"
          width="75"
        /><el-table-column label="创建时间" width="165"
          ><template #default="scope">{{
            dayjs(scope.row.createdAt).format("YYYY-MM-DD HH:mm")
          }}</template></el-table-column
        ></el-table
      >
    </section></template
  >
</template>
