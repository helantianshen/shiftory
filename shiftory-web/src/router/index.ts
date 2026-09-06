import { createRouter, createWebHistory } from "vue-router";
import { useSessionStore } from "@/stores/session";

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: "/auth",
      name: "auth",
      component: () => import("@/views/AuthView.vue"),
      meta: { public: true },
    },
    {
      path: "/",
      component: () => import("@/layouts/AppLayout.vue"),
      redirect: "/overview",
      children: [
        {
          path: "overview",
          component: () => import("@/views/OverviewView.vue"),
        },
        {
          path: "schedule",
          component: () => import("@/views/ScheduleView.vue"),
        },
        { path: "import", component: () => import("@/views/ImportView.vue") },
        {
          path: "imports",
          component: () => import("@/views/ImportHistoryView.vue"),
        },
        {
          path: "imports/:id",
          component: () => import("@/views/ImportReviewView.vue"),
        },
        {
          path: "calendar",
          component: () => import("@/views/CalendarView.vue"),
        },
        { path: "profile", component: () => import("@/views/ProfileView.vue") },
        { path: "invitations", component: () => import("@/views/InvitationsView.vue") },
        {
          path: "admin/schedules",
          component: () => import("@/views/AdminScheduleView.vue"),
          meta: { admin: true },
        },
        {
          path: "admin/members",
          component: () => import("@/views/MembersView.vue"),
          meta: { admin: true },
        },
        {
          path: "admin/shifts",
          component: () => import("@/views/ShiftsView.vue"),
          meta: { admin: true },
        },
        {
          path: "admin/imports",
          component: () => import("@/views/ImportHistoryView.vue"),
          meta: { admin: true, allImports: true },
        },
        {
          path: "admin/workspace",
          component: () => import("@/views/WorkspaceSettingsView.vue"),
          meta: { admin: true },
        },
      ],
    },
    { path: "/:pathMatch(.*)*", redirect: "/overview" },
  ],
});

router.beforeEach(async (to) => {
  const session = useSessionStore();
  await session.bootstrap();
  if (!to.meta.public && !session.isAuthenticated)
    return { name: "auth", query: { redirect: to.fullPath } };
  if (to.meta.public && session.isAuthenticated) return "/overview";
  if (to.meta.admin && !session.isAdmin) return "/overview";
  return true;
});
export default router;
