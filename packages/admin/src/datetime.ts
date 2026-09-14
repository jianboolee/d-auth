export function pad2(n: number) {
  return String(n).padStart(2, "0");
}

/**
 * 把后端返回的 ISO 时间格式化为本地时间，精确到分钟：YYYY-MM-DD HH:mm。
 * 传入空值或非法值时返回 fallback，避免界面出现 Invalid Date。
 */
export function formatDateTime(value?: string | null, fallback = "—") {
  if (!value) return fallback;
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return fallback;
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())} ${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
}

/** 只要日期：YYYY-MM-DD。 */
export function formatDate(value?: string | null, fallback = "—") {
  if (!value) return fallback;
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return fallback;
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`;
}
