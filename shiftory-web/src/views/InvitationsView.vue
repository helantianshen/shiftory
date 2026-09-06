<script setup lang="ts">
import { useQuery } from "@tanstack/vue-query";
import { ElMessage } from "element-plus";
import { api } from "@/api/client";
import PageHeader from "@/components/PageHeader.vue";
const invitations = useQuery({ queryKey: ["my-invitations"], queryFn: () => api.get<{items: {id:number; workspaceId:number; workspaceName:string; role:string; expiresAt:string}[]}>("/invitations/mine") });
async function accept(id: number) { await api.post("/invitations/accept", { invitationId: id }); await invitations.refetch(); ElMessage.success("已加入工作区"); }
</script>
<template><PageHeader eyebrow="INVITATIONS" title="我的邀请" description="查看并接受其他工作区发来的邀请。" /><section class="surface-card table-card"><el-empty v-if="!invitations.data.value?.items.length" description="暂无待处理邀请" /><el-table v-else :data="invitations.data.value.items"><el-table-column prop="workspaceName" label="工作区" /><el-table-column prop="role" label="角色" width="120" /><el-table-column prop="expiresAt" label="有效期至" /><el-table-column label="操作" width="120"><template #default="scope"><el-button type="primary" size="small" @click="accept(scope.row.id)">接受</el-button></template></el-table-column></el-table></section></template>

