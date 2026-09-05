import { describe, expect, it } from "vitest";

import {
  IMPORT_CATEGORY_GUIDE,
  IMPORT_COMMIT_CONFIRM_MESSAGE,
  IMPORT_COMMIT_CONFIRM_OPTIONS,
  importCategoryLabel,
  importStateLabel,
} from "./import-review-content";

describe("import review content", () => {
  it("uses concise Chinese commit confirmation content", () => {
    expect(IMPORT_COMMIT_CONFIRM_MESSAGE).toBe(
      "提交后将写入所有选中的排班；不确定、无效和缺失项不会写入。",
    );
    expect(IMPORT_COMMIT_CONFIRM_OPTIONS.confirmButtonText).toBe("确认");
    expect(IMPORT_COMMIT_CONFIRM_OPTIONS.cancelButtonText).toBe("取消");
  });

  it("explains every supported import classification", () => {
    expect(IMPORT_CATEGORY_GUIDE.map((item) => item.type)).toEqual([
      "NEW",
      "SAME",
      "CONFLICT",
      "MISSING",
      "UNCERTAIN",
      "INVALID",
    ]);
    expect(IMPORT_CATEGORY_GUIDE.every((item) => item.description.length > 0)).toBe(true);
  });

  it("renders import classifications and states in lowercase", () => {
    expect(importCategoryLabel("MISSING")).toBe("missing");
    expect(importCategoryLabel("UNCERTAIN")).toBe("uncertain");
    expect(importStateLabel("NEEDS_REVIEW")).toBe("needs_review");
  });
});
