<script setup lang="ts">
import { computed, ref, watch } from "vue";
import dayjs from "dayjs";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  LayoutDashboard,
  CalendarDays,
  Clock3,
  FileClock,
  Import,
  Settings,
  Users,
  UserRound,
  WandSparkles,
  ChevronDown,
  ChevronRight,
  ChevronLeft,
  Bell,
  Menu,
  Plus,
  ArrowUpRight,
  Coffee,
  AlertCircle,
  UploadCloud,
  CalendarCheck,
  Sun,
  Moon,
  X,
  UserCog,
} from "lucide-vue-next";

const route = ref(location.hash.slice(1) || "/overview");
const menuOpen = ref(false);
const dark = ref(false);
const workspace = "产品研发组";
const navigation = [
  {
    label: "",
    items: [{ to: "/overview", label: "概览", icon: LayoutDashboard }],
  },
  {
    label: "个人",
    items: [
      { to: "/schedule", label: "我的排班", icon: Clock3 },
      { to: "/profile", label: "个人资料", icon: UserRound },
    ],
  },
  {
    label: "工作区",
    items: [
      { to: "/import", label: "导入排班", icon: Import },
      { to: "/calendar", label: "团队日历", icon: CalendarDays },
      { to: "/imports", label: "我的导入记录", icon: FileClock },
      { to: "/admin/schedules", label: "排班管理", icon: WandSparkles },
      { to: "/admin/members", label: "成员管理", icon: Users },
      { to: "/admin/shifts", label: "班次设置", icon: Clock3 },
      { to: "/admin/imports", label: "全部导入记录", icon: FileClock },
      { to: "/admin/workspace", label: "工作区设置", icon: Settings },
    ],
  },
];
const titles: Record<string, string> = {
  "/overview": "概览",
  "/calendar": "团队日历",
  "/admin/members": "成员管理",
};
const title = computed(() => titles[route.value] || "概览");
const placeholder = ref("");
const month = ref(dayjs("2026-11-01"));
const selectedMembers = ref<number[]>([]);
const onlyRest = ref(false);
const onlyMissing = ref(false);
const selectedDay = ref<any>(null);
const drawer = ref(false);
const invitationOpen = ref(false);
const invitationMode = ref("email");
const invitationTarget = ref("");
const invitationRole = ref("MEMBER");
const token = ref("");
const members = ref([
  {
    id: 1,
    name: "林予安",
    email: "lin.yuan@example.com",
    role: "OWNER",
    status: "ACTIVE",
    completion: 100,
    imported: "2026-10-31 16:42",
    joined: "2026-09-01",
    color: "green",
  },
  {
    id: 2,
    name: "陈知远",
    email: "chen.zhiyuan@example.com",
    role: "ADMIN",
    status: "ACTIVE",
    completion: 100,
    imported: "2026-10-30 09:18",
    joined: "2026-09-02",
    color: "blue",
  },
  {
    id: 3,
    name: "苏晚晴",
    email: "su.wanqing@example.com",
    role: "MEMBER",
    status: "ACTIVE",
    completion: 93,
    imported: "2026-10-31 14:26",
    joined: "2026-09-05",
    color: "purple",
  },
  {
    id: 4,
    name: "周屿",
    email: "zhou.yu@example.com",
    role: "MEMBER",
    status: "ACTIVE",
    completion: 80,
    imported: "2026-10-29 11:03",
    joined: "2026-09-12",
    color: "orange",
  },
  {
    id: 5,
    name: "许清和",
    email: "xu.qinghe@example.com",
    role: "MEMBER",
    status: "ACTIVE",
    completion: 100,
    imported: "2026-10-31 10:35",
    joined: "2026-09-18",
    color: "blue",
  },
  {
    id: 6,
    name: "沈一宁",
    email: "shen.yining@example.com",
    role: "MEMBER",
    status: "DISABLED",
    completion: 67,
    imported: "2026-10-25 15:12",
    joined: "2026-10-01",
    color: "purple",
  },
]);
const invitations = ref([
  { id: 1, target: "li.ming@example.com", role: "MEMBER", status: "PENDING" },
  { id: 2, target: "wang_xiao（用户名）", role: "MEMBER", status: "ACCEPTED" },
]);
const roleNames: Record<string, string> = {
  OWNER: "所有者",
  ADMIN: "管理员",
  MEMBER: "普通成员",
};
const stats = [
  { label: "今日工作", value: "3", unit: "人", icon: Users, tone: "blue" },
  { label: "今日休息", value: "2", unit: "人", icon: Coffee, tone: "green" },
  {
    label: "排班缺失",
    value: "1",
    unit: "人",
    icon: AlertCircle,
    tone: "orange",
  },
  {
    label: "待确认导入",
    value: "2",
    unit: "项",
    icon: UploadCloud,
    tone: "purple",
  },
];
// 日历由固定演示数据生成，筛选和月份切换仅作用于原型
const days = computed(() =>
  Array.from({ length: month.value.daysInMonth() }, (_, i) => {
    const date = month.value.date(i + 1);
    const commonRest = [7, 14, 21, 28].includes(i + 1);
    const roster = members.value
      .filter(
        (m) =>
          !selectedMembers.value.length || selectedMembers.value.includes(m.id),
      )
      .map((m, j) => ({
        ...m,
        state: commonRest
          ? "rest"
          : (i + j) % 7 === 5
            ? "missing"
            : (i + j) % 4 === 0
              ? "rest"
              : "work",
      }));
    const missing = roster.filter((m) => m.state === "missing").length;
    const rest = roster.filter((m) => m.state === "rest").length;
    return {
      date: date.format("YYYY-MM-DD"),
      day: i + 1,
      col: ((date.day() + 6) % 7) + 1,
      row:
        Math.floor((((month.value.startOf("month").day() + 6) % 7) + i) / 7) +
        1,
      roster,
      missing,
      rest,
      working: roster.length - rest - missing,
      allRest: rest === roster.length,
      today: date.format("YYYY-MM-DD") === "2026-11-01",
    };
  }).filter(
    (d) =>
      (!onlyRest.value || d.allRest) && (!onlyMissing.value || d.missing > 0),
  ),
);
watch(
  dark,
  (value) => {
    document.documentElement.classList.toggle("dark", value);
  },
  { immediate: true },
);
window.addEventListener("hashchange", () => {
  if (titles[location.hash.slice(1)]) route.value = location.hash.slice(1);
});
// 未制作的页面保留原入口，通过说明弹窗标明本次原型边界
function navigate(path: string, label: string) {
  menuOpen.value = false;
  if (titles[path]) {
    route.value = path;
    location.hash = path;
    window.scrollTo(0, 0);
  } else placeholder.value = label;
}
function openDay(day: any) {
  selectedDay.value = day;
  drawer.value = true;
}
// 邀请和成员操作只更新内存中的演示数据
function createInvitation() {
  if (
    !invitationTarget.value.trim() ||
    (invitationMode.value === "email" &&
      !/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(invitationTarget.value))
  ) {
    ElMessage.warning(
      "请填写有效的" + (invitationMode.value === "email" ? "邮箱" : "用户名"),
    );
    return;
  }
  invitations.value.unshift({
    id: Date.now(),
    target:
      invitationTarget.value +
      (invitationMode.value === "username" ? "（用户名）" : ""),
    role: invitationRole.value,
    status: "PENDING",
  });
  token.value = "demo-invitation-token";
  ElMessage.success("演示邀请已创建，未发送实际邀请");
}
async function removeMember(member: (typeof members.value)[number]) {
  try {
    await ElMessageBox.confirm(
      `移除 ${member.name}？历史排班会保留。`,
      "移除成员",
      { confirmButtonText: "确认", cancelButtonText: "取消", type: "warning" },
    );
    member.status = "REMOVED";
  } catch {
    /* 取消时保留演示数据 */
  }
}
</script>

<template>
  <div class="app-shell">
    <button
      v-if="menuOpen"
      class="sidebar-mask"
      aria-label="关闭菜单"
      @click="menuOpen = false"
    ></button>
    <aside class="sidebar" :class="{ open: menuOpen }">
      <a
        href="#/overview"
        class="brand"
        @click.prevent="navigate('/overview', '概览')"
        ><span class="brand-mark"><CalendarCheck :size="23" /></span
        ><span>Shiftory<span class="brand-caption">排班协同</span></span></a
      >
      <button
        class="mobile-close icon-button"
        aria-label="关闭菜单"
        @click="menuOpen = false"
      >
        <X :size="18" />
      </button>
      <nav aria-label="主导航">
        <section
          v-for="group in navigation"
          :key="group.label"
          class="nav-group"
        >
          <div v-if="group.label" class="nav-label">{{ group.label }}</div>
          <a
            v-for="item in group.items"
            :key="item.to"
            :href="'#' + item.to"
            class="nav-item"
            :class="{ active: route === item.to }"
            :aria-current="route === item.to ? 'page' : undefined"
            @click.prevent="navigate(item.to, item.label)"
            ><component :is="item.icon" :size="18" /><span>{{
              item.label
            }}</span
            ><ChevronRight
              v-if="route === item.to"
              :size="14"
              class="nav-arrow"
          /></a>
        </section>
      </nav>
      <div class="sidebar-footer">
        <span class="footer-mark"><CalendarDays :size="17" /></span>
        <div>清晰排班，轻松协同<small>个人共享 · 团队协作</small></div>
      </div>
    </aside>
    <div class="main-shell">
      <header class="topbar">
        <button
          class="icon-button mobile-toggle"
          aria-label="打开菜单"
          @click="menuOpen = true"
        >
          <Menu :size="20" />
        </button>
        <div class="breadcrumb">
          <span>工作区</span><ChevronRight :size="13" /><strong>{{
            title
          }}</strong>
        </div>
        <div class="topbar-right">
          <button
            class="icon-button"
            :aria-label="dark ? '切换浅色模式' : '切换深色模式'"
            :title="dark ? '切换浅色模式' : '切换深色模式'"
            @click="dark = !dark"
          >
            <Sun v-if="dark" :size="19" /><Moon v-else :size="19" />
          </button>
          <button
            class="icon-button notification"
            aria-label="通知"
            @click="placeholder = '我的邀请'"
          >
            <Bell :size="19" /><i></i>
          </button>
          <el-dropdown trigger="click" @command="placeholder = $event"
            ><button class="profile-button">
              <span class="avatar">林</span
              ><span class="profile-text">林予安<small>所有者</small></span
              ><ChevronDown :size="13" /></button
            ><template #dropdown
              ><el-dropdown-menu
                ><el-dropdown-item command="个人资料">个人资料</el-dropdown-item
                ><el-dropdown-item divided command="退出登录"
                  >退出登录</el-dropdown-item
                ></el-dropdown-menu
              ></template
            ></el-dropdown
          >
        </div>
      </header>
      <main>
        <template v-if="route === '/overview'">
          <div class="page-heading">
            <div>
              <h1>今天，一目了然</h1>
              <p>
                {{ workspace
                }}<span class="dot-separator">·</span>2026年11月01日，星期日
              </p>
            </div>
            <el-button type="primary" @click="navigate('/import', '导入排班')"
              ><UploadCloud :size="16" />导入排班</el-button
            >
          </div>
          <div class="stats-grid">
            <article
              v-for="stat in stats"
              :key="stat.label"
              class="stat-card panel"
            >
              <div class="stat-top">
                <span>{{ stat.label }}</span
                ><span class="stat-icon" :class="stat.tone"
                  ><component :is="stat.icon" :size="20"
                /></span>
              </div>
              <div class="stat-number">
                {{ stat.value }}<span>{{ stat.unit }}</span>
              </div>
              <div class="stat-footer">
                <span class="status-dot" :class="stat.tone"></span
                >{{
                  stat.label === "待确认导入"
                    ? "当前工作区的待处理任务"
                    : "当前工作区 · 今日排班"
                }}
              </div>
            </article>
          </div>
          <div class="overview-grid">
            <section class="panel personal-panel">
              <div class="panel-title">
                <div>
                  <h2>我的排班</h2>
                  <p>个人排班，全局共享</p>
                </div>
                <span class="subtle-icon"><CalendarDays :size="20" /></span>
              </div>
              <div class="completion">
                <el-progress
                  type="dashboard"
                  :percentage="93"
                  :width="184"
                  :stroke-width="12"
                  color="var(--accent)"
                  ><div class="completion-value">93<span>%</span></div>
                  <div class="completion-caption">本月完整度</div></el-progress
                >
                <p>每一天的安排，都有迹可循</p>
              </div>
              <div class="next-days">
                <div>
                  <span class="detail-icon blue"><Users :size="17" /></span>
                  <div>
                    <span>下一个工作日</span
                    ><strong>11月02日 <small>星期一</small></strong>
                  </div>
                </div>
                <div>
                  <span class="detail-icon green"><Coffee :size="17" /></span>
                  <div>
                    <span>下一个休息日</span
                    ><strong>11月07日 <small>星期六</small></strong>
                  </div>
                </div>
              </div>
            </section>
            <section class="panel team-panel">
              <div class="panel-title">
                <div>
                  <h2>团队状态</h2>
                  <p>仅统计当前工作区成员</p>
                </div>
                <span class="subtle-icon"><Users :size="20" /></span>
              </div>
              <div class="team-summary">
                <div>
                  <strong>5<span> / 6 人</span></strong>
                  <p>今日已明确排班</p>
                </div>
                <span class="status-pill orange"
                  ><AlertCircle :size="13" />1 人仍需补全</span
                >
              </div>
              <div class="distribution">
                <span class="work" style="width: 50%"></span
                ><span class="rest" style="width: 33.333%"></span
                ><span class="missing" style="width: 16.667%"></span>
              </div>
              <div class="legend">
                <span><i class="status-dot blue"></i>工作 3 人</span
                ><span><i class="status-dot green"></i>休息 2 人</span
                ><span><i class="status-dot orange"></i>缺失 1 人</span>
              </div>
              <div class="rest-heading">
                <h3>近期共同休息</h3>
                <span>2026年11月</span>
              </div>
              <div class="rest-dates">
                <div v-for="d in [7, 14, 21, 28]" :key="d">
                  <span>星期六</span
                  ><strong>{{ String(d).padStart(2, "0") }}</strong
                  ><small>全部休息</small>
                </div>
              </div>
              <button
                class="calendar-link"
                @click="navigate('/calendar', '团队日历')"
              >
                打开团队日历<ArrowUpRight :size="17" />
              </button>
            </section>
          </div>
        </template>
        <template v-else-if="route === '/calendar'">
          <div class="page-heading">
            <div>
              <h1>团队日历</h1>
              <p>
                未选择成员时显示全部成员；共同休息要求所有成员明确休息且没有缺失。
              </p>
            </div>
          </div>
          <section class="panel calendar-panel">
            <div class="calendar-toolbar">
              <div class="month-switch">
                <button
                  class="icon-button"
                  aria-label="上个月"
                  @click="month = month.subtract(1, 'month')"
                >
                  <ChevronLeft :size="18" />
                </button>
                <h2>{{ month.format("YYYY年 M月") }}</h2>
                <button
                  class="icon-button"
                  aria-label="下个月"
                  @click="month = month.add(1, 'month')"
                >
                  <ChevronRight :size="18" />
                </button>
              </div>
              <el-select
                v-model="selectedMembers"
                multiple
                collapse-tags
                clearable
                filterable
                placeholder="全部成员（可选择筛选）"
                class="member-select"
                ><el-option
                  v-for="m in members"
                  :key="m.id"
                  :label="
                    m.name + (m.status === 'ACTIVE' ? '' : '（历史成员）')
                  "
                  :value="m.id"
              /></el-select>
              <div class="calendar-filters">
                <el-checkbox v-model="onlyRest">只看全部休息</el-checkbox
                ><el-checkbox v-model="onlyMissing">只看数据缺失</el-checkbox>
              </div>
            </div>
            <div class="calendar-scroll">
              <div class="weekdays">
                <span
                  v-for="day in [
                    '周一',
                    '周二',
                    '周三',
                    '周四',
                    '周五',
                    '周六',
                    '周日',
                  ]"
                  :key="day"
                  >{{ day }}</span
                >
              </div>
              <div class="calendar-grid">
                <button
                  v-for="d in days"
                  :key="d.date"
                  class="day-cell"
                  :class="{
                    'all-rest': d.allRest,
                    today: d.today,
                    overdue: d.date < '2026-11-01',
                  }"
                  :style="{ gridColumn: d.col, gridRow: d.row }"
                  :aria-label="d.date + ' 成员详情'"
                  @click="openDay(d)"
                >
                  <div class="day-number">
                    <span>{{ String(d.day).padStart(2, "0") }}</span
                    ><small v-if="d.today">今天</small
                    ><Coffee v-else-if="d.allRest" :size="15" />
                  </div>
                  <div class="day-status" :class="{ common: d.allRest }">
                    <span v-if="d.allRest">全部休息</span
                    ><template v-else
                      ><span
                        >工作 <b>{{ d.working }}</b></span
                      ><span
                        >休息 <b>{{ d.rest }}</b></span
                      ></template
                    >
                  </div>
                  <small v-if="d.missing" class="day-missing"
                    ><span></span>缺失 {{ d.missing }} 人</small
                  >
                </button>
              </div>
              <el-empty
                v-if="!days.length"
                description="没有符合筛选条件的日期"
                :image-size="75"
              />
            </div>
            <div class="calendar-bottom">
              <div class="legend">
                <span><i class="status-dot green"></i>全部休息</span
                ><span><i class="status-dot orange"></i>排班缺失</span>
              </div>
              <span>点击日期查看成员排班</span>
            </div>
          </section>
        </template>
        <template v-else-if="route === '/admin/members'">
          <div class="page-heading">
            <div>
              <h1>成员管理</h1>
              <p>禁用或移除只终止访问，不删除历史排班。</p>
            </div>
            <el-button
              type="primary"
              @click="
                invitationOpen = true;
                token = '';
                invitationTarget = '';
              "
              ><Plus :size="17" />邀请成员</el-button
            >
          </div>
          <section class="panel table-panel">
            <div class="table-title">
              <h2>
                工作区成员 <span class="count">{{ members.length }}</span>
              </h2>
              <span>{{ workspace }}</span>
            </div>
            <el-table :data="members" style="width: 100%"
              ><el-table-column label="成员" min-width="235"
                ><template #default="{ row }"
                  ><div class="member-info">
                    <span class="member-avatar" :class="row.color">{{
                      row.name.slice(0, 1)
                    }}</span>
                    <div>
                      <strong>{{ row.name }}</strong
                      ><small>{{ row.email }}</small>
                    </div>
                  </div></template
                ></el-table-column
              ><el-table-column label="角色" width="100"
                ><template #default="{ row }"
                  ><span
                    class="role-label"
                    :class="{ owner: row.role === 'OWNER' }"
                    >{{ roleNames[row.role] }}</span
                  ></template
                ></el-table-column
              ><el-table-column label="状态" width="100"
                ><template #default="{ row }"
                  ><span
                    class="status-pill"
                    :class="row.status === 'ACTIVE' ? 'green' : 'neutral'"
                    ><i></i
                    >{{
                      row.status === "ACTIVE"
                        ? "正常"
                        : row.status === "DISABLED"
                          ? "已禁用"
                          : "已移除"
                    }}</span
                  ></template
                ></el-table-column
              ><el-table-column label="本月完整度" width="140"
                ><template #default="{ row }"
                  ><el-progress
                    :percentage="row.completion"
                    :stroke-width="5"
                    :color="
                      row.completion === 100 ? 'var(--accent)' : '#d69c47'
                    " /></template></el-table-column
              ><el-table-column
                prop="imported"
                label="最近导入"
                width="155"
              /><el-table-column
                prop="joined"
                label="加入时间"
                width="115"
              /><el-table-column label="操作" width="230"
                ><template #default="{ row }"
                  ><div v-if="row.role !== 'OWNER'" class="row-actions">
                    <el-button
                      text
                      @click="
                        row.status =
                          row.status === 'ACTIVE' ? 'DISABLED' : 'ACTIVE'
                      "
                      >{{
                        row.status === "ACTIVE" ? "禁用" : "启用"
                      }}</el-button
                    ><el-button
                      text
                      type="primary"
                      @click="
                        row.role = row.role === 'ADMIN' ? 'MEMBER' : 'ADMIN'
                      "
                      >{{
                        row.role === "ADMIN" ? "取消管理员" : "设为管理员"
                      }}</el-button
                    ><el-button text type="danger" @click="removeMember(row)"
                      >移除</el-button
                    >
                  </div>
                  <span v-else class="owner-note"
                    ><UserCog :size="14" />所有者</span
                  ></template
                ></el-table-column
              ></el-table
            >
          </section>
          <section class="panel table-panel invitations-panel">
            <div class="table-title">
              <h2>
                邀请记录 <span class="count">{{ invitations.length }}</span>
              </h2>
            </div>
            <el-table :data="invitations"
              ><el-table-column
                label="目标"
                prop="target"
                min-width="240"
              /><el-table-column label="角色" width="150"
                ><template #default="{ row }">{{
                  roleNames[row.role]
                }}</template></el-table-column
              ><el-table-column label="状态" width="170"
                ><template #default="{ row }"
                  ><span
                    class="status-pill"
                    :class="row.status === 'PENDING' ? 'orange' : 'green'"
                    >{{ row.status === "PENDING" ? "待接受" : "已接受" }}</span
                  ></template
                ></el-table-column
              ><el-table-column label="操作" width="130"
                ><template #default="{ row }"
                  ><el-button
                    v-if="row.status === 'PENDING'"
                    text
                    type="danger"
                    @click="
                      invitations = invitations.filter((i) => i.id !== row.id)
                    "
                    >撤销</el-button
                  ><span v-else class="muted">—</span></template
                ></el-table-column
              ></el-table
            >
          </section>
        </template>
        <footer class="page-footer">
          <span>Shiftory · 排班协同</span><span>视觉原型 · 演示数据</span>
        </footer>
      </main>
    </div>
  </div>
  <el-drawer
    v-model="drawer"
    :title="`${selectedDay?.date ?? ''} · 成员详情`"
    size="min(480px, 96vw)"
    ><div class="drawer-summary">
      <CalendarDays :size="21" /><span
        >工作 {{ selectedDay?.working }} 人 · 休息 {{ selectedDay?.rest }} 人 ·
        缺失 {{ selectedDay?.missing }} 人</span
      >
    </div>
    <div v-for="m in selectedDay?.roster" :key="m.id" class="roster-row">
      <div class="member-info">
        <span class="member-avatar" :class="m.color">{{
          m.name.slice(0, 1)
        }}</span
        ><strong>{{ m.name }}</strong>
      </div>
      <span :class="['roster-state', m.state]">{{
        m.state === "rest"
          ? "休息"
          : m.state === "missing"
            ? "未填写排班"
            : "09:00 – 18:00"
      }}</span>
    </div></el-drawer
  >
  <el-dialog v-model="invitationOpen" title="邀请成员" width="min(480px, 94vw)"
    ><p class="dialog-description">邀请新成员加入「{{ workspace }}」。</p>
    <el-form label-position="top"
      ><el-form-item label="邀请方式"
        ><el-radio-group v-model="invitationMode"
          ><el-radio-button value="email">邮箱</el-radio-button
          ><el-radio-button value="username"
            >用户名</el-radio-button
          ></el-radio-group
        ></el-form-item
      ><el-form-item :label="invitationMode === 'email' ? '邮箱' : '用户名'"
        ><el-input
          v-model="invitationTarget"
          :placeholder="
            invitationMode === 'email' ? '输入邮箱地址' : '输入已注册用户名'
          " /></el-form-item
      ><el-form-item label="角色"
        ><el-select v-model="invitationRole"
          ><el-option label="普通成员" value="MEMBER" /><el-option
            label="管理员"
            value="ADMIN" /></el-select></el-form-item
      ><el-alert
        v-if="token"
        title="邀请令牌（演示）"
        type="success"
        :closable="false"
        :description="token" /></el-form
    ><template #footer
      ><el-button @click="invitationOpen = false">关闭</el-button
      ><el-button type="primary" @click="createInvitation"
        >创建邀请</el-button
      ></template
    ></el-dialog
  >
  <el-dialog
    :model-value="!!placeholder"
    :title="placeholder"
    width="min(420px,94vw)"
    @close="placeholder = ''"
    ><p class="dialog-description">
      此入口保持不变。本次仅预览概览、团队日历和成员管理，其他页面将在后续适配。
    </p>
    <template #footer
      ><el-button type="primary" @click="placeholder = ''"
        >知道了</el-button
      ></template
    ></el-dialog
  >
</template>
