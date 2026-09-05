import type { ImportItem } from "@/api/types";

export const IMPORT_COMMIT_CONFIRM_MESSAGE =
  "提交后将写入所有选中的排班；不确定、无效和缺失项不会写入。";

export const IMPORT_COMMIT_CONFIRM_OPTIONS = {
  type: "warning",
  confirmButtonText: "确认",
  cancelButtonText: "取消",
} as const;

export function importCategoryLabel(type: ImportItem["type"]): string {
  return type.toLowerCase();
}

export function importStateLabel(state: string): string {
  return state.toLowerCase();
}

export const IMPORT_CATEGORY_GUIDE: ReadonlyArray<{
  type: ImportItem["type"];
  description: string;
}> = [
  { type: "NEW", description: "新增排班，默认写入" },
  { type: "SAME", description: "与现有排班一致，无需修改" },
  { type: "CONFLICT", description: "与现有排班不同，需要选择" },
  { type: "MISSING", description: "文件中缺少该日期，不写入" },
  { type: "UNCERTAIN", description: "内容无法确认，需要人工修正" },
  { type: "INVALID", description: "数据不符合规则，不写入" },
];
