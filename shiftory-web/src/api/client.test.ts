import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiClient, ApiError, formatApiError } from "./client";

describe("ApiClient", () => {
  afterEach(() => vi.restoreAllMocks());

  it("rotates access JWT through the HttpOnly refresh flow and retries once", async () => {
    document.cookie = "shiftory_csrf=csrf-value; path=/";
    let token: string | null = "expired-token";
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ error: { code: "INVALID_TOKEN" } }), {
          status: 401,
          headers: { "Content-Type": "application/json" },
        }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ data: { accessToken: "fresh-token" } }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ data: { items: [] } }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient(
      () => token,
      (value) => (token = value),
    );

    const result = await client.get<{ items: unknown[] }>("/workspaces");

    expect(result.items).toEqual([]);
    expect(token).toBe("fresh-token");
    expect(fetchMock).toHaveBeenCalledTimes(3);
    const refreshHeaders = new Headers(fetchMock.mock.calls[1]?.[1]?.headers);
    expect(refreshHeaders.get("X-CSRF-Token")).toBe("csrf-value");
    const retriedHeaders = new Headers(fetchMock.mock.calls[2]?.[1]?.headers);
    expect(retriedHeaders.get("Authorization")).toBe("Bearer fresh-token");
  });

  it("formats structured workbook validation details for users", () => {
    const error = new ApiError(400, {
      code: "INVALID_WORKBOOK",
      message: "休息日不能填写班次、开始时间、结束时间或跨日",
      details: {
        row: 2,
        fields: ["状态", "班次"],
        ruleCode: "REST_HAS_SEGMENTS",
        hint: "请清空班次、开始时间和结束时间，并将是否跨日设为“否”",
      },
    });

    expect(formatApiError(error, "上传失败")).toBe(
      "第 2 行（状态、班次）：休息日不能填写班次、开始时间、结束时间或跨日。请清空班次、开始时间和结束时间，并将是否跨日设为“否”",
    );
  });
});
