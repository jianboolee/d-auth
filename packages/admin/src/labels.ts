const statusLabels: Record<string, string> = {
  active: "正常",
  disabled: "已停用",
  deleted: "已删除",
};

const roleLabels: Record<string, string> = {
  user: "用户",
  admin: "管理员",
  superadmin: "超级管理员",
};

const identityLabels: Record<string, string> = {
  phone: "手机",
  email: "邮箱",
  wechat_open: "微信",
  wechat_mini: "微信小程序",
};

const clientTypeLabels: Record<string, string> = {
  web: "网站",
  miniprogram: "小程序",
  native: "App",
};

export const roleOptions = [
  { label: "用户", value: "user" },
  { label: "管理员", value: "admin" },
  { label: "超级管理员", value: "superadmin" },
];

export const clientTypeOptions = [
  { label: "网站", value: "web" },
  { label: "小程序", value: "miniprogram" },
  { label: "App", value: "native" },
];

/** API Key 有效期选项；值为天数，0 表示不过期。 */
export const expiryOptions = [
  { label: "永不过期", value: 0 },
  { label: "30 天", value: 30 },
  { label: "90 天", value: 90 },
  { label: "1 年", value: 365 },
];

export function statusLabel(v: string) {
  return statusLabels[v] ?? v;
}

export function roleLabel(v: string) {
  return roleLabels[v] ?? v;
}

export function identityLabel(v: string) {
  return identityLabels[v] ?? v;
}

export function clientTypeLabel(v: string) {
  return clientTypeLabels[v] ?? v;
}

export function errMessage(e: unknown, fallback = "操作失败") {
  if (e && typeof e === "object" && "message" in e && typeof (e as { message: unknown }).message === "string") {
    return (e as { message: string }).message;
  }
  return fallback;
}
