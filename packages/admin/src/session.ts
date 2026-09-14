import { inject, provide, type InjectionKey, type Ref } from "vue";
import type { User } from "@donktech/d-auth-sdk";

export type AuthSession = {
  user: Ref<User | null>;
  setUser: (user: User | null) => void;
};

const KEY: InjectionKey<AuthSession> = Symbol("dauth-session");

export function provideAuthSession(session: AuthSession) {
  provide(KEY, session);
}

export function useAuthSession(): AuthSession {
  const s = inject(KEY);
  if (!s) {
    throw new Error("未提供登录态，请在应用根节点调用 provideAuthSession");
  }
  return s;
}
