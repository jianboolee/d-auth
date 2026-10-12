/**
 * 复制文本到剪贴板，返回是否成功。
 *
 * 优先用异步 Clipboard API；它只在安全上下文（HTTPS 或 localhost）可用，控制台
 * 若挂在 http 的 IP 上会没有 navigator.clipboard，因此退回 execCommand 的老办法。
 */
export async function copyText(text: string): Promise<boolean> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // 用户拒绝授权或浏览器策略拦截，继续走降级方案。
    }
  }
  try {
    const el = document.createElement("textarea");
    el.value = text;
    // 固定在视口内但不可见，避免页面滚动；iOS 上 readonly 会干扰选中。
    el.style.position = "fixed";
    el.style.top = "0";
    el.style.left = "0";
    el.style.opacity = "0";
    document.body.appendChild(el);
    el.select();
    const ok = document.execCommand("copy");
    document.body.removeChild(el);
    return ok;
  } catch {
    return false;
  }
}