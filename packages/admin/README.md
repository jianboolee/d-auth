# @donktech/d-auth-admin

与本站控制台同一套用户管理页面，可嵌入其它 Vue3 后台。

依赖 `@donktech/d-auth-sdk`、`vue`、`vue-router`、`naive-ui`。

```ts
import { createDAuthClient } from "@donktech/d-auth-sdk";
import { createAuthAdminRoutes, setAdminClient } from "@donktech/d-auth-admin";

const auth = createDAuthClient({
  baseURL: "https://auth.x-rv.com",
  mode: "cookie",
  clientKey: "web_admin",
});
setAdminClient(auth);

const routes = createAuthAdminRoutes({
  basePath: "/admin",
  variant: "users", // 只要用户/角色；"full" 再加客户端、API Key、审计
});
```

安装：`"@donktech/d-auth-admin": "file:../d-auth/packages/admin"`。
