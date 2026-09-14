import type { RouteRecordRaw } from "vue-router";
import { setAdminClient, useAdminClient } from "./client";
import { provideAuthSession, useAuthSession } from "./session";
import { useAuthPaths, useEmbed } from "./paths";
import UsersPage from "./pages/UsersPage.vue";
import UserDetailPage from "./pages/UserDetailPage.vue";
import RolesPage from "./pages/RolesPage.vue";
import ClientsPage from "./pages/ClientsPage.vue";
import ApiKeysPage from "./pages/ApiKeysPage.vue";
import AuditPage from "./pages/AuditPage.vue";
import CreateUserPage from "./pages/CreateUserPage.vue";
import LoginPage from "./pages/auth/LoginPage.vue";
import RegisterPage from "./pages/auth/RegisterPage.vue";
import ForgotPage from "./pages/auth/ForgotPage.vue";
import TermsPage from "./pages/legal/TermsPage.vue";
import PrivacyPage from "./pages/legal/PrivacyPage.vue";
import ProfilePage from "./pages/account/ProfilePage.vue";
import SecurityPage from "./pages/account/SecurityPage.vue";
import IdentitiesPage from "./pages/account/IdentitiesPage.vue";
import SessionsPage from "./pages/account/SessionsPage.vue";

export { setAdminClient, useAdminClient, provideAuthSession, useAuthSession, useAuthPaths, useEmbed };
export {
  UsersPage,
  UserDetailPage,
  RolesPage,
  ClientsPage,
  ApiKeysPage,
  AuditPage,
  CreateUserPage,
  LoginPage,
  RegisterPage,
  ForgotPage,
  TermsPage,
  PrivacyPage,
  ProfilePage,
  SecurityPage,
  IdentitiesPage,
  SessionsPage,
};

export type AdminRouteOptions = {
  basePath?: string;
  variant?: "users" | "full";
};

function stripBase(path: string, base: string) {
  const p = path.replace(new RegExp(`^${base}/?`), "");
  return p || "";
}

export function createAuthAdminRoutes(opts: AdminRouteOptions = {}): RouteRecordRaw[] {
  const base = opts.basePath ?? "/admin";
  const variant = opts.variant ?? "users";
  const routes: RouteRecordRaw[] = [
    { path: `${base}/users`, component: UsersPage, props: { basePath: base }, meta: { title: "用户" } },
    { path: `${base}/users/new`, component: CreateUserPage, props: { basePath: base }, meta: { title: "创建用户" } },
    { path: `${base}/users/:id`, component: UserDetailPage, props: { basePath: base }, meta: { title: "用户详情" } },
    { path: `${base}/roles`, component: RolesPage, meta: { title: "角色" } },
  ];
  if (variant === "full") {
    routes.push(
      { path: `${base}/clients`, component: ClientsPage, meta: { title: "应用" } },
      { path: `${base}/api-keys`, component: ApiKeysPage, meta: { title: "接口密钥" } },
      { path: `${base}/audit`, component: AuditPage, meta: { title: "审计" } },
    );
  }
  return routes;
}

export function createAuthAccountRoutes(opts: { basePath?: string } = {}): RouteRecordRaw[] {
  const base = opts.basePath ?? "/account";
  return [
    { path: `${base}/profile`, component: ProfilePage, meta: { title: "个人资料" } },
    { path: `${base}/security`, component: SecurityPage, meta: { title: "安全" } },
    { path: `${base}/identities`, component: IdentitiesPage, meta: { title: "登录方式" } },
    { path: `${base}/sessions`, component: SessionsPage, meta: { title: "会话" } },
  ];
}

export function createAuthPublicRoutes(opts: { basePath?: string; embed?: boolean } = {}): RouteRecordRaw[] {
  const base = opts.basePath ?? "";
  const embed = opts.embed === true;
  const p = (s: string) => (base ? `${base}${s}` : s);
  return [
    { path: p("/login"), component: LoginPage, meta: { public: true, embed, title: "登录" } },
    { path: p("/register"), component: RegisterPage, meta: { public: true, embed, title: "注册" } },
    { path: p("/forgot"), component: ForgotPage, meta: { public: true, embed, title: "重置密码" } },
  ];
}

/** 用户协议 / 隐私政策：无 Shell layout，登录态也可访问 */
export function createAuthLegalRoutes(): RouteRecordRaw[] {
  return [
    { path: "/terms", component: TermsPage, meta: { legal: true, title: "用户协议" } },
    { path: "/privacy", component: PrivacyPage, meta: { legal: true, title: "隐私政策" } },
  ];
}

export function asChildRoutes(routes: RouteRecordRaw[], base: string): RouteRecordRaw[] {
  return routes.map((r) => ({
    path: stripBase(String(r.path), base),
    component: r.component,
    props: r.props,
    meta: r.meta,
  }));
}
