import { createApp } from "vue";
import { createPinia } from "pinia";
import ElementPlus from "element-plus";
import zhCn from "element-plus/es/locale/lang/zh-cn";
import "element-plus/dist/index.css";
import { VueQueryPlugin } from "@tanstack/vue-query";
import App from "./App.vue";
import router from "./router";
import { api } from "./api/client";
import { useSessionStore } from "./stores/session";
import "./styles/main.scss";

const app = createApp(App);
const pinia = createPinia();
app.use(pinia);
app.use(router);
app.use(ElementPlus, { locale: zhCn });
app.use(VueQueryPlugin, {
  queryClientConfig: {
    defaultOptions: { queries: { staleTime: 30_000, retry: 1 } },
  },
});

const session = useSessionStore(pinia);
api.configureTokenAccess(
  () => session.accessToken,
  (token) => session.setAccessToken(token),
);
session.applyPreferences({ currentWorkspaceId: null, theme: session.theme });

app.mount("#app");
