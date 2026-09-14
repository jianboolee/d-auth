import { createDAuthClient } from "@donktech/d-auth-sdk";

export const dauth = createDAuthClient({
  baseURL: "",
  mode: "cookie",
  clientKey: "web_console",
});
