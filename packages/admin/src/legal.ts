import DOMPurify from "dompurify";
import { marked } from "marked";
import type { LegalDoc, LegalDocSet } from "@donktech/d-auth-sdk";
import { useAdminClient } from "./client";

export type LegalKind = "terms" | "privacy";

/**
 * 只放行 http/https 绝对地址。
 *
 * 这个值会被交给 window.location.replace()，若放行 javascript: 之类伪协议，
 * 就等于在所有访问者都会加载的页面上执行 XSS。服务端已有一道同样的校验，
 * 这里是纵深防御（也保护不经过服务端校验的历史数据）。
 */
export function isSafeUrl(url: string): boolean {
  const v = url.trim().toLowerCase();
  return v.startsWith("http://") || v.startsWith("https://");
}

/**
 * 把 markdown 渲染成已消毒的 HTML。
 *
 * 正文由管理员在后台填写，但渲染结果会展示给所有用户——管理员账号一旦被接管，
 * 未消毒的正文就是存储型 XSS。因此必须过 DOMPurify。
 */
export function renderMarkdown(md: string): string {
  const html = marked.parse(md, { async: false }) as string;
  return DOMPurify.sanitize(html);
}

let cache: Promise<LegalDocSet> | null = null;

/**
 * 读取法务文档。公开接口，未登录也能调（登录页会链到这两个页面）。
 * 一次会话内缓存：协议内容极少变化，没必要每个页面各拉一次。
 */
export function loadLegal(): Promise<LegalDocSet> {
  if (!cache) {
    cache = useAdminClient()
      .legal()
      .catch((e) => {
        // 失败时清掉缓存，让下次访问可以重试，而不是永久卡在错误态。
        cache = null;
        throw e;
      });
  }
  return cache;
}

/** 取单个文档；不存在时返回空文档。 */
export async function loadLegalDoc(kind: LegalKind): Promise<LegalDoc> {
  const all = await loadLegal();
  return all[kind] ?? { kind, url: "", content: "", updated_at: "" };
}

/** 后台保存后调用，避免下次打开前台读到旧内容。 */
export function invalidateLegalCache(): void {
  cache = null;
}
