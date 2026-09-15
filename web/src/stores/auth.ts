import { defineStore } from "pinia";
import { dauth } from "../api/client";
import type { User } from "@donktech/d-auth-sdk";

export const useAuthStore = defineStore("auth", {
  state: () => ({
    user: null as User | null,
    loaded: false,
  }),
  getters: {
    isAdmin: (s) => !!s.user?.roles?.some((r) => r === "admin" || r === "superadmin"),
    isSuper: (s) => !!s.user?.roles?.includes("superadmin"),
  },
  actions: {
    async bootstrap() {
      try {
        // 探针接口未登录时返回空对象而不是 401，因此这里不会产生多余的
        // 401 与刷新请求。
        const res = await dauth.session();
        this.user = res.user ?? null;
      } catch {
        this.user = null;
      } finally {
        this.loaded = true;
      }
    },
    async logout() {
      await dauth.logout();
      this.user = null;
    },
    /** 会话已在服务端失效（如过期）时，只清本地状态，不再调用接口。 */
    clear() {
      this.user = null;
    },
  },
});
