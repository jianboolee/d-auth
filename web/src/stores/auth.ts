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
        this.user = await dauth.me();
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
  },
});
