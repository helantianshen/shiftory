# 任务：主题同步侧边栏

## 目标

修复主题切换后左侧深绿色菜单栏不变化的问题。

## 当前状态

已完成。

## 修改内容

- `shiftory-web/src/styles/main.scss`：将侧边栏背景、文字、导航和底部文字颜色改为主题 CSS 变量，并为晴空、丁香、樱粉、琥珀、石墨主题配置对应深色调色板。
- `shiftory-web/src/styles/themes.ts`：集中维护主题对应的侧边栏背景色。
- `shiftory-web/src/layouts/AppLayout.vue`：侧边栏绑定当前 Pinia 主题的背景变量，切换主题后立即更新。
- `shiftory-web/src/styles/themes.test.ts`：验证六套主题都有独立侧边栏颜色。

## 验证

- 前端完整测试：6 个测试文件、11 个测试通过。
- 前端类型检查：通过。
- 前端生产构建：通过；保留既有约 894 kB 主 chunk 警告。

## 设计说明

主题切换仍由 Pinia 的 `selectTheme` 负责；本次只补齐侧边栏视觉变量，不改变主题持久化或后端偏好接口。
