import { createRouter, createWebHistory } from "vue-router";
import {
  asChildRoutes,
  createAuthAccountRoutes,
  createAuthAdminRoutes,
  createAuthPublicRoutes,
} from "@donktech/d-auth-admin";
import { useAuthStore } from "../stores/auth";

const Shell = () => import("../layouts/ShellLayout.vue");

const router = createRouter({
  history: createWebHistory(),
  routes: [
    ...createAuthPublicRoutes(),
    {
      path: "/account",
      component: Shell,
      children: [
        { path: "", redirect: "/account/profile" },
        ...asChildRoutes(createAuthAccountRoutes({ basePath: "/account" }), "/account"),
      ],
    },
    {
      path: "/admin",
      component: Shell,
      meta: { admin: true },
      children: [
        { path: "", redirect: "/admin/users" },
        ...asChildRoutes(createAuthAdminRoutes({ basePath: "/admin", variant: "full" }), "/admin"),
      ],
    },
    ...createAuthPublicRoutes({ basePath: "/embed", embed: true }).map((r) => ({
      ...r,
      component: Shell,
      children: [{ path: "", component: r.component, meta: r.meta }],
    })),
    {
      path: "/embed/account",
      component: Shell,
      meta: { embed: true },
      children: [
        { path: "", redirect: "/embed/account/profile" },
        ...asChildRoutes(createAuthAccountRoutes({ basePath: "/embed/account" }), "/embed/account").map((r) => ({
          ...r,
          meta: { ...r.meta, embed: true },
        })),
      ],
    },
    {
      path: "/embed/admin",
      component: Shell,
      meta: { admin: true, embed: true },
      children: [
        { path: "", redirect: "/embed/admin/users" },
        ...asChildRoutes(createAuthAdminRoutes({ basePath: "/embed/admin", variant: "full" }), "/embed/admin").map((r) => ({
          ...r,
          meta: { ...r.meta, embed: true, admin: true },
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
    if (auth.user) return to.path.startsWith("/embed") ? "/embed/account/profile" : "/account/profile";
    return true;
  }
  if (!auth.user) return to.path.startsWith("/embed") ? "/embed/login" : "/login";
  if (to.meta.admin && !auth.isAdmin) {
    return to.path.startsWith("/embed") ? "/embed/account/profile" : "/account/profile";
  }
  return true;
});

export default router;
