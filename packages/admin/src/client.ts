import type { DAuthClient } from "@donktech/d-auth-sdk";

let client: DAuthClient | null = null;

export function setAdminClient(c: DAuthClient) {
  client = c;
}

export function useAdminClient(): DAuthClient {
  if (!client) {
    throw new Error("d-auth admin client is not set. Call setAdminClient() first.");
  }
  return client;
}
