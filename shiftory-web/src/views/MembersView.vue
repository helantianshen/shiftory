<script setup lang="ts">
import { computed, reactive, ref } from "vue";
import { useQuery, useQueryClient } from "@tanstack/vue-query";
import { ElMessage, ElMessageBox } from "element-plus";
import dayjs from "dayjs";
import { Plus, UserCog } from "lucide-vue-next";
import { api } from "@/api/client";
import type { Member } from "@/api/types";
import PageHeader from "@/components/PageHeader.vue";
import { useSessionStore } from "@/stores/session";
const session = useSessionStore();
const workspaceID = computed(() => session.currentWorkspace!.id);
const queryClient = useQueryClient();
const invitationOpen = ref(false);
const invitation = reactive({ mode: "email", email: "", username: "", role: "MEMBER" });
const inviteResult = ref("");
const members = useQuery({
  queryKey: computed(() => ["members", workspaceID.value]),
  queryFn: () =>
    api.get<{ items: Member[] }>(`/workspaces/${workspaceID.value}/members`),
});
const invitations = useQuery({
  queryKey: computed(() => ["invitations", workspaceID.value]),
  queryFn: () =>
    api.get<{
      items: {
        id: number;
        email: string;
        role: string;
        status: string;
        expiresAt: string;
      }[];
    }>(`/workspaces/${workspaceID.value}/invitations`),
});
async function createInvite() {
  const payload = invitation.mode === "username"
    ? { username: invitation.username, role: invitation.role }
    : { email: invitation.email, role: invitation.role };
  const result = await api.post<{ token: string }>(
    `/workspaces/${workspaceID.value}/invitations`,
    payload,
  );
  inviteResult.value = result.token;
  ElMessage.success("邀请已创建");
  await invitations.refetch();
}
async function update(member: Member, role: string, status: string) {
  await api.patch(`/workspaces/${workspaceID.value}/members/${member.id}`, {
    role,
    status,
  });
  ElMessage.success("成员状态已更新");
  await queryClient.invalidateQueries({
    queryKey: ["members", workspaceID.value],
  });
}
async function remove(member: Member) {
  await ElMessageBox.confirm(
    `移除 ${member.displayName}？历史排班会保留。`,
    "移除成员",
    { type: "warning" },
  );
  await update(
    member,
    member.role === "OWNER" ? "MEMBER" : member.role,
    "REMOVED",
  );
}
async function revoke(id: number) {
  await api.delete(`/workspaces/${workspaceID.value}/invitations/${id}`);
  await invitations.refetch();
}
</script>
<template>
  <PageHeader
    eyebrow="MEMBERS"
    title="成员管理"
    description="禁用或移除只终止访问，不删除历史排班。"
    ><el-button type="primary" @click="invitationOpen = true"
      ><Plus :size="16" />邀请成员</el-button
    ></PageHeader
  >
  <section class="surface-card table-card">
    <el-table :data="members.data.value?.items ?? []"
      ><el-table-column label="成员" min-width="190"
        ><template #default="scope"
          ><strong>{{ scope.row.displayName }}</strong>
          <div class="muted">{{ scope.row.email }}</div></template
        ></el-table-column
      ><el-table-column prop="role" label="角色" width="110" /><el-table-column
        prop="status"
        label="状态"
        width="110"
      /><el-table-column label="本月完整度" width="130"
        ><template #default="scope"
          ><el-progress
            :percentage="scope.row.scheduleCompleteness ?? 0"
            :stroke-width="8" /></template></el-table-column
      ><el-table-column label="最近导入" width="150"
        ><template #default="scope">{{
          scope.row.lastImportAt
            ? dayjs(scope.row.lastImportAt).format("YYYY-MM-DD HH:mm")
            : "—"
        }}</template></el-table-column
      ><el-table-column label="加入时间" width="140"
        ><template #default="scope">{{
          dayjs(scope.row.joinedAt).format("YYYY-MM-DD")
        }}</template></el-table-column
      ><el-table-column label="操作" width="300"
        ><template #default="scope"
          ><template v-if="scope.row.role !== 'OWNER'"
            ><el-button
              size="small"
              @click="
                update(
                  scope.row,
                  scope.row.role,
                  scope.row.status === 'ACTIVE' ? 'DISABLED' : 'ACTIVE',
                )
              "
              >{{ scope.row.status === "ACTIVE" ? "禁用" : "启用" }}</el-button
            ><el-button
              v-if="session.isOwner"
              size="small"
              @click="
                update(
                  scope.row,
                  scope.row.role === 'ADMIN' ? 'MEMBER' : 'ADMIN',
                  scope.row.status,
                )
              "
              >{{
                scope.row.role === "ADMIN" ? "取消管理员" : "设为管理员"
              }}</el-button
            ><el-button
              size="small"
              type="danger"
              plain
              @click="remove(scope.row)"
              >移除</el-button
            ></template
          ><span v-else class="muted"
            ><UserCog :size="15" />所有者</span
          ></template
        ></el-table-column
      ></el-table
    >
  </section>
  <section class="surface-card table-card" style="margin-top: 18px">
    <div class="card-padding"><h2 class="section-title">邀请记录</h2></div>
    <el-table :data="invitations.data.value?.items ?? []"
      ><el-table-column label="目标" min-width="200"><template #default="scope"><span v-if="scope.row.username">{{ scope.row.username }}（用户名）</span><span v-else>{{ scope.row.email }}</span></template></el-table-column><el-table-column
        prop="role"
        label="角色"
        width="100"
      /><el-table-column
        prop="status"
        label="状态"
        width="110"
      /><el-table-column label="操作" width="100"
        ><template #default="scope"
          ><el-button
            v-if="scope.row.status === 'PENDING'"
            text
            type="danger"
            @click="revoke(scope.row.id)"
            >撤销</el-button
          ></template
        ></el-table-column
      ></el-table
    >
  </section>
  <el-dialog v-model="invitationOpen" title="邀请成员" width="min(500px,94vw)"
    ><el-form label-position="top"
      ><el-form-item label="邀请方式"><el-radio-group v-model="invitation.mode"><el-radio-button value="email">邮箱</el-radio-button><el-radio-button value="username">用户名</el-radio-button></el-radio-group></el-form-item><el-form-item :label="invitation.mode === 'username' ? '用户名' : '邮箱'"><el-input v-if="invitation.mode === 'username'" v-model="invitation.username" placeholder="输入已注册用户名" /><el-input v-else v-model="invitation.email" placeholder="输入邮箱地址" /></el-form-item
      ><el-form-item label="角色"
        ><el-select v-model="invitation.role"
          ><el-option label="普通成员" value="MEMBER" /><el-option
            label="管理员"
            value="ADMIN" /></el-select></el-form-item
      ><el-alert
        v-if="inviteResult"
        type="success"
        :closable="false"
        title="邀请令牌"
        ><p style="word-break: break-all">{{ inviteResult }}</p></el-alert
      ></el-form
    ><template #footer
      ><el-button @click="invitationOpen = false">关闭</el-button
      ><el-button type="primary" @click="createInvite"
        >创建邀请</el-button
      ></template
    ></el-dialog
  >
</template>
