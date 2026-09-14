import { createRouter, createWebHistory } from "vue-router";
import { createAuthAdminRoutes } from "@donktech/d-auth-admin";
import { useAuthStore } from "../stores/auth";

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/login", component: () => import("../views/LoginView.vue"), meta: { public: true } },
    { path: "/register", component: () => import("../views/RegisterView.vue"), meta: { public: true } },
    { path: "/forgot", component: () => import("../views/ForgotView.vue"), meta: { public: true } },
    {
      path: "/account",
      component: () => import("../layouts/ShellLayout.vue"),
      children: [
        { path: "", redirect: "/account/profile" },
        { path: "profile", component: () => import("../views/account/ProfileView.vue") },
        { path: "security", component: () => import("../views/account/SecurityView.vue") },
        { path: "sessions", component: () => import("../views/account/SessionsView.vue") },
        { path: "identities", component: () => import("../views/account/IdentitiesView.vue") },
      ],
    },
    {
      path: "/admin",
      component: () => import("../layouts/ShellLayout.vue"),
      meta: { admin: true },
      children: [
        { path: "", redirect: "/admin/users" },
        ...createAuthAdminRoutes({ basePath: "/admin", variant: "full" }).map((r) => ({
          path: r.path.replace(/^\/admin\/?/, "") || "",
          component: r.component,
          props: r.props,
          meta: r.meta,
        })),
      ],
    },
    { path: "/", redirect: "/account/profile" },
  ],
});

router.beforeEach(async (to) => {
  const auth = useAuthStore();
  if (!auth.loaded) await auth.bootstrap();
  if (to.meta.public) {
    if (auth.user) return "/account/profile";
    return true;
  }
  if (!auth.user) return "/login";
  if (to.meta.admin && !auth.isAdmin) return "/account/profile";
  return true;
});

export default router;
