/**
 * 验证码发送相关的公共判断。
 */

type ApiErrorLike = { code?: string; status?: number };

/**
 * 判断错误是否为「发送被限流」（SEND_TOO_FAST / SEND_LIMIT）。
 * 用于让前端在命中服务端限流时也进入倒计时，避免用户反复点击被连续拒绝。
 * 这纯粹是体验优化——真正的限制在服务端，前端倒计时可被刷新绕过。
 */
export function isRateLimited(e: unknown): boolean {
  const err = e as ApiErrorLike | null;
  if (!err) return false;
  return err.code === "SEND_TOO_FAST" || err.code === "SEND_LIMIT" || err.status === 429;
}
