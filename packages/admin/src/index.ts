import type { RouteRecordRaw } from "vue-router";
import { setAdminClient, useAdminClient } from "./client";
import UsersPage from "./pages/UsersPage.vue";
import UserDetailPage from "./pages/UserDetailPage.vue";
import RolesPage from "./pages/RolesPage.vue";
import ClientsPage from "./pages/ClientsPage.vue";
import ApiKeysPage from "./pages/ApiKeysPage.vue";
import AuditPage from "./pages/AuditPage.vue";
import CreateUserPage from "./pages/CreateUserPage.vue";

export { setAdminClient, useAdminClient };
export { UsersPage, UserDetailPage, RolesPage, ClientsPage, ApiKeysPage, AuditPage, CreateUserPage };

export type AdminRouteOptions = {
  basePath?: string;
  variant?: "users" | "full";
};

export function createAuthAdminRoutes(opts: AdminRouteOptions = {}): RouteRecordRaw[] {
  const base = opts.basePath ?? "/admin";
  const variant = opts.variant ?? "users";
  const routes: RouteRecordRaw[] = [
    { path: `${base}/users`, component: UsersPage, props: { basePath: base }, meta: { title: "用户" } },
    { path: `${base}/users/new`, component: CreateUserPage, meta: { title: "创建用户" } },
    { path: `${base}/users/:id`, component: UserDetailPage, props: { basePath: base }, meta: { title: "用户详情" } },
    { path: `${base}/roles`, component: RolesPage, meta: { title: "角色" } },
  ];
  if (variant === "full") {
    routes.push(
      { path: `${base}/clients`, component: ClientsPage, meta: { title: "应用" } },
      { path: `${base}/api-keys`, component: ApiKeysPage, meta: { title: "API Key" } },
      { path: `${base}/audit`, component: AuditPage, meta: { title: "审计" } },
    );
  }
  return routes;
}
