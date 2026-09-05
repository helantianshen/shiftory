# 任务：导入审核页中文化与分类说明

## 目标

将确认导入对话框完全中文化，使用精简提示，并在任务概览中解释导入分类。

## 当前状态

已完成。

## 修改内容

- `shiftory-web/src/views/import-review-content.ts`：集中维护确认文案、按钮文案和六种分类说明。
- `shiftory-web/src/views/ImportReviewView.vue`：确认框使用“确认/取消”和精简提示；概览卡片增加分类说明网格。
- `shiftory-web/src/main.ts`：Element Plus 全局切换为简体中文，其他确认框的默认按钮也显示中文。
- `shiftory-web/src/views/import-review-content.test.ts`：验证文案和分类覆盖。

## 验证

- 前端完整测试：5 个测试文件、9 个测试通过。
- 前端类型检查：通过。
- 前端生产构建：通过；保留既有约 894 kB 主 chunk 警告，不影响构建。

## 说明

- 将用户给出的“提交后将以写入”修正为语义完整的“提交后将写入”。
- 分类说明仅解释当前导入处理状态，不改变后端分类和写入规则。
