import { createApp } from "vue";
import ElementPlus from "element-plus";
import zhCn from "element-plus/es/locale/lang/zh-cn";
import "element-plus/dist/index.css";
import "element-plus/theme-chalk/dark/css-vars.css";
import Prototype from "./Prototype.vue";
import "./style.css";
createApp(Prototype).use(ElementPlus, { locale: zhCn }).mount("#app");
