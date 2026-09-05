# 任务：导入状态小写显示

## 目标

将导入审核页中的分类和导入状态从英文大写显示改为小写，同时不改变后端 API 枚举值。

## 当前状态

已完成。

## 修改内容

- `shiftory-web/src/views/import-review-content.ts`：新增分类和状态显示格式化函数，统一调用 `toLowerCase()`。
- `shiftory-web/src/views/ImportReviewView.vue`：概览状态和分类列改为小写显示；内部条件判断继续使用原始大写枚举。
- `shiftory-web/src/views/import-review-content.test.ts`：新增小写显示回归测试。

## 验证

- 前端完整测试：5 个测试文件、10 个测试通过。
- 前端类型检查：通过。
- 前端生产构建：通过；保留既有约 894 kB 主 chunk 警告。

## 设计说明

只调整展示层，不修改 `ImportItem.type`、`ImportJob.state` 或后端数据库/API 的大写枚举，避免破坏接口兼容性和业务判断。
