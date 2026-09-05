# 任务：导入错误前端展示

## 目标

将后端导入校验返回的行号、字段和修复提示展示给用户。

## 当前状态

已完成。API 客户端提供统一错误格式化函数，Excel/图片导入页面复用该函数。

## 修改文件

- `shiftory-web/src/api/client.ts`：新增 `formatApiError`，解析 `ApiError.body.details` 的 `row`、`fields`、`hint`。
- `shiftory-web/src/views/ImportView.vue`：上传失败时使用格式化后的可读错误。
- `shiftory-web/src/api/client.test.ts`：新增结构化工作簿错误展示测试。

## 验证

- `npm exec --yes --package pnpm@11.19.0 -- pnpm --dir shiftory-web type-check`：通过。
- `npm exec --yes --package pnpm@11.19.0 -- pnpm --dir shiftory-web test`：通过，4 个测试文件、7 个测试。
- `npm exec --yes --package pnpm@11.19.0 -- pnpm --dir shiftory-web build-only`：通过；保留既有约 890 kB 主 chunk 警告，不影响构建。

## 约束与风险

- 后端仍负责最终校验，前端只负责展示错误，不改变授权或校验边界。
- 当前只在导入页接入格式化函数，其他页面保留原有错误展示方式；后续新增页面应复用 `formatApiError`。
