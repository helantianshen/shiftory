# Shiftory 界面原型

本原型用 Art Design Pro 的浅色设计样式适配 Shiftory 的概览、团队日历和成员管理。它独立于正式前端入口，使用项目已有的 Vue、Element Plus 和 Lucide，不访问后端接口。

在 `shiftory-web` 目录运行：

```sh
pnpm exec vite --config prototype/vite.config.ts
```

打开 http://127.0.0.1:5179/ 。三页对应 `#/overview`、`#/calendar`、`#/admin/members`。

```sh
pnpm exec vite build --config prototype/vite.config.ts
```

可预览默认蓝色的浅色／深色模式、手机菜单、月份和成员筛选、日历日期详情、成员状态与角色操作、邀请创建和撤销。所有数据为演示数据，刷新即恢复；邀请不会实际发送。演示身份为工作区所有者。其他原有菜单入口保持名称、顺序和分组，点击展示本次原型范围说明。

正式接入时应保留现有路由守卫、会话、工作区数据范围、请求和缓存机制，将展示层适配到现有功能和组件。原型中所有内存操作与演示身份均不能替代正式授权逻辑。

视觉依据：Art Design Pro 的 `src/assets/styles/core/tailwind.css`、`app.scss`、`el-ui.scss` 和 `src/components/core/layouts/art-menus/art-sidebar-menu/theme.scss`。保留了它的灰阶、细边框、圆角菜单与紧凑控件风格；使用固定蓝色主色，支持浅色和深色模式。右上角不显示工作区选择器。上游 MIT 许可见 `THIRD_PARTY_LICENSES.txt`。
