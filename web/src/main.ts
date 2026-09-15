import { createApp } from "vue";
import { createPinia } from "pinia";
import naive from "naive-ui";
import { setAdminClient } from "@donktech/d-auth-admin";
import App from "./App.vue";
import router from "./router";
import { dauth } from "./api/client";
import { useAuthStore } from "./stores/auth";
import "./assets/theme.css";
import "remixicon/fonts/remixicon.css";

setAdminClient(dauth);

const app = createApp(App);
app.use(createPinia());
app.use(router);

// 会话在服务端失效（过期 / 被吊销）时把用户带回登录页。
// 此前没有任何 401 处理，会话失效后页面只会不断报接口错误而不跳转。
// 只处理 UNAUTHORIZED：登录接口密码错误也是 401，但那不该触发跳转。
dauth.setUnauthorizedHandler(() => {
  const auth = useAuthStore();
  auth.clear();
  const current = router.currentRoute.value;
  if (current.meta.public || current.meta.legal) return; // 已在登录/协议页，避免循环
  const target = current.path.startsWith("/embed") ? "/embed/login" : "/login";
  if (current.path !== target) router.replace(target);
});

app.use(naive);
app.mount("#app");
