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

const loginMethodLabels: Record<string, string> = {
  sms: "短信",
  email: "邮箱",
  wechat_qr: "微信扫码",
  wechat_mini: "微信小程序",
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

export const loginMethodOptions = [
  { label: "短信", value: "sms" },
  { label: "邮箱", value: "email" },
  { label: "微信扫码", value: "wechat_qr" },
  { label: "微信小程序", value: "wechat_mini" },
];

export const scopeOptions = [
  { label: "读取用户", value: "users:read" },
  { label: "管理用户", value: "users:write" },
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

export function loginMethodLabel(v: string) {
  return loginMethodLabels[v] ?? v;
}

export function errMessage(e: unknown, fallback = "操作失败") {
  if (e && typeof e === "object" && "message" in e && typeof (e as { message: unknown }).message === "string") {
    return (e as { message: string }).message;
  }
  return fallback;
}
