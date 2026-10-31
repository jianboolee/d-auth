/**
 * 手机号输入框的公共处理。
 *
 * 后端 normalize.Phone 支持「本地 11 位」「带 86」「+86」「+ 国际号码」多种写法，
 * 前端不应该把这些写法截断掉——之前各处用 `replace(/\D/g,"").slice(0,11)`，
 * 粘贴 +8613800138000 会被截成 86138001380，反而变成非法号码。
 */

/**
 * 用于没有固定国家码前缀的输入框（如后台直接改手机号）：
 * 保留开头的 +，其余只留数字，最长 15 位（E.164 上限）。不改变用户输入的形态，
 * 由后端做最终规范化与校验。
 */
export function normalizePhoneInput(value: string): string {
  const plus = value.trim().startsWith("+") ? "+" : "";
  return plus + value.replace(/\D/g, "").slice(0, 15);
}

/**
 * 用于界面上已显示固定「+86」前缀的输入框：只收本地 11 位。
 * 若粘贴的是完整号码（+86 / 86 开头），先去掉国家码再截断，避免被截成乱码。
 */
export function toLocalCNPhone(value: string): string {
  let digits = value.replace(/\D/g, "");
  if (digits.length > 11 && digits.startsWith("86")) digits = digits.slice(2);
  return digits.slice(0, 11);
}

/**
 * 校验手机号是否可用，与后端 normalize.Phone 的可接受范围保持一致：
 * 本地 11 位、带 86 前缀（后面接本地号）、或 + 国际号码。
 * 这里的规则需要与 normalize.Phone 同步，否则会出现「前端说不行、后端说行」的矛盾。
 *
 * 面向用户的自助流程（注册、登录、改绑手机号）用它；后台管理入口用
 * adminPhoneLooksValid，两者刻意不同。
 */
export function phoneLooksValid(value: string): boolean {
  const v = value.trim().replace(/[\s-]/g, "");
  if (/^1[3-9]\d{9}$/.test(v)) return true;
  if (/^86(1[3-9]\d{9})$/.test(v)) return true;
  return /^\+\d{8,15}$/.test(v);
}

/**
 * 后台建号／改绑手机号使用的宽松校验，与后端 normalize.AdminPhone 对应：
 * 在真实号码之外，还接受任意 6-15 位纯数字，以便存下 120 开头的虚拟号。
 *
 * 只用于后台的管理员操作；「生成虚拟号」产生的号码收不到短信验证码，
 * 只能用于密码登录（见后端 normalize.PasswordIdentifier）。
 */
export function adminPhoneLooksValid(value: string): boolean {
  if (phoneLooksValid(value)) return true;
  return /^\d{6,15}$/.test(value.trim().replace(/[\s-]/g, ""));
}

/**
 * 生成虚拟手机号失败时兜底用的本地号码，形态与后端一致（120 + 8 位）。
 * 正常应调用 SDK 的 adminGenerateVirtualPhone，由服务端保证不重复。
 */
export function randomVirtualPhone(): string {
  const digits = String(Math.floor(Math.random() * 1e8)).padStart(8, "0");
  return `120${digits}`;
}
