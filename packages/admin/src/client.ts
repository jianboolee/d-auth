import type { DAuthClient } from "@donktech/d-auth-sdk";

let client: DAuthClient | null = null;

export function setAdminClient(c: DAuthClient) {
  client = c;
}

export function useAdminClient(): DAuthClient {
  if (!client) {
    throw new Error("尚未设置账户客户端，请先调用 setAdminClient");
  }
  return client;
}
