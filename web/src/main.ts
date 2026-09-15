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

// 组件渲染抛错时，Vue 默认只打一条警告，router-view 会停在上一个成功渲染的
// 页面：地址栏变了、标题也变了（标题在路由钩子里设），内容却不变，切任何路由
// 都没反应，看起来就像「页面卡住了」。
//
// 之前用户列表就因为一条没有角色的记录（roles 为 null）触发过这个现象。这里
// 把错误统一打到控制台并带上组件与阶段，至少让这类故障可定位，而不是静默冻结。
app.config.errorHandler = (err, _instance, info) => {
  console.error(`[d-auth] 组件错误（${info}）`, err);
};

app.mount("#app");
