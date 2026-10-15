import { createRouter, createWebHistory } from "vue-router";
import type { RouteRecordRaw } from "vue-router";
import {
  asChildRoutes,
  createAuthAccountRoutes,
  createAuthAdminRoutes,
  createAuthBindPhoneRoutes,
  createAuthLegalRoutes,
  createAuthPublicRoutes,
} from "@donktech/d-auth-admin";
import { useAuthStore } from "../stores/auth";

const Shell = () => import("../layouts/ShellLayout.vue");

const router = createRouter({
  history: createWebHistory(),
  routes: <RouteRecordRaw[]>[
    ...createAuthPublicRoutes(),
    ...createAuthLegalRoutes(),
    // 绑定手机号：无侧栏的独立页；不再是强制门槛，用户可从账户安全进入。
    ...createAuthBindPhoneRoutes(),
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
        { path: "", redirect: "/admin/dashboard" },
        ...asChildRoutes(createAuthAdminRoutes({ basePath: "/admin", variant: "full" }), "/admin"),
      ],
    },
    ...createAuthPublicRoutes({ basePath: "/embed", embed: true }).map((r) => ({
      ...r,
      component: Shell,
      children: [{ path: "", component: r.component, meta: r.meta }],
    })),
    ...createAuthBindPhoneRoutes({ basePath: "/embed/account", embed: true }).map((r) => ({
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
        { path: "", redirect: "/embed/admin/dashboard" },
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
  const embed = to.path.startsWith("/embed");
  const accountHome = embed ? "/embed/account/profile" : "/account/profile";

  if (to.meta.legal) return true;

  // 已登录用户访问登录/注册页：直接回账户首页。
  if (to.meta.public) {
    if (!auth.user) return true;
    return accountHome;
  }

  if (!auth.user) return embed ? "/embed/login" : "/login";

  // 手机号不再强制绑定：未绑定的用户可以正常进入账户页；
  // 绑定手机号页保留为可选项（账户安全里入口），已绑定的再访问则回首页。
  if (to.meta.bindPhone && auth.user.has_phone) return accountHome;
  if (to.meta.admin && !auth.isAdmin) return accountHome;
  return true;
});

/**
 * 页面标题统一由路由决定：每个路由在 meta.title 里声明自己的名字，这里直接
 * 写进 document.title，不再拼接站点名。放在路由而不是各页面组件里，是因为
 * 组件卸载时机不可靠——约定俗成的做法是让新页面覆盖旧标题。
 *
 * 没有 meta.title 的路由（未匹配地址等）回退到站点名，避免标题空着。
 */
const SITE_TITLE = "账户中心";

router.afterEach((to) => {
  const page = typeof to.meta.title === "string" ? to.meta.title.trim() : "";
  document.title = page || SITE_TITLE;
});

export default router;
