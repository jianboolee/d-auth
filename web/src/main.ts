import { createApp } from "vue";
import { createPinia } from "pinia";
import naive from "naive-ui";
import { setAdminClient } from "@donktech/d-auth-admin";
import App from "./App.vue";
import router from "./router";
import { dauth } from "./api/client";

setAdminClient(dauth);

const app = createApp(App);
app.use(createPinia());
app.use(router);
app.use(naive);
app.mount("#app");
