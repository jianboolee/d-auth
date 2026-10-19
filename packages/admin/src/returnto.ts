import type { RouteLocationNormalized } from "vue-router";
import { useAdminClient } from "./client";

/**
 * 从当前路由的 query 里解析子系统 SSO 回跳地址。
 *
 * 约定：子系统跳登录页时带上 `client`（应用标识）与 `return_to`（登录成功后
 * 要回跳的完整 URL）。是否允许回跳由 d-auth 服务端按 origin 白名单判定
 * （`GET /v1/auth/return-target`）——前端不接触白名单，只消费判定结果，
 * 防开放重定向的边界留在服务端。
 *
 * 任何异常（缺参数、接口失败、URL 非法）一律返回 null，调用方据此退回
 * 常规登录后行为（进账户中心）。
 */
export async function resolveReturnTo(route: RouteLocationNormalized): Promise<string | null> {
  const client = typeof route.query.client === "string" ? route.query.client : "";
  const returnTo = typeof route.query.return_to === "string" ? route.query.return_to : "";
  if (!client || !returnTo) return null;
  try {
    const res = await useAdminClient().resolveReturnTarget(client, returnTo);
    if (!res.allowed || !res.url) return null;
    // 服务端已校验过 origin，这里只把明显不是 http(s) 的畸形结果再挡一道。
    const u = new URL(res.url, window.location.origin);
    if (u.protocol !== "http:" && u.protocol !== "https:") return null;
    return u.toString();
  } catch {
    return null;
  }
}
