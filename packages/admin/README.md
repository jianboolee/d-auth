# @donktech/d-auth-admin

与本站控制台同一套页面，可嵌入其它 Vue3 后台或小程序 web-view（打开 `/embed/...`）。

依赖 `@donktech/d-auth-sdk`、`vue`、`vue-router`、`naive-ui`。宿主需引入 `remixicon/fonts/remixicon.css`，并 `provideAuthSession`。

```ts
import { createDAuthClient } from "@donktech/d-auth-sdk";
import {
  createAuthAdminRoutes,
  createAuthAccountRoutes,
  setAdminClient,
  provideAuthSession,
} from "@donktech/d-auth-admin";

const auth = createDAuthClient({
  baseURL: "https://auth.x-rv.com",
  mode: "cookie",
  clientKey: "web_admin",
});
setAdminClient(auth);

const routes = [
  ...createAuthAccountRoutes({ basePath: "/account" }),
  ...createAuthAdminRoutes({ basePath: "/admin", variant: "users" }),
];
```

安装：`"@donktech/d-auth-admin": "file:../d-auth/packages/admin"`。
