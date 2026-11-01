import { ref } from "vue";

// 明暗模式属于本机显示偏好，不占用服务端配色字段
export const darkMode = ref(
  localStorage.getItem("shiftory-color-mode") === "dark",
);
document.documentElement.classList.toggle("dark", darkMode.value);

// 同步页面明暗模式并保存本机选择
export function toggleColorMode() {
  darkMode.value = !darkMode.value;
  document.documentElement.classList.toggle("dark", darkMode.value);
  localStorage.setItem(
    "shiftory-color-mode",
    darkMode.value ? "dark" : "light",
  );
}
