export interface ApiErrorBody {
  code: string;
  message: string;
  details?: unknown;
  requestId?: string;
}

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly body: ApiErrorBody,
  ) {
    super(body.message || `API request failed with status ${status}`);
  }
}

export function formatApiError(error: unknown, fallback = "操作失败"): string {
  if (!(error instanceof ApiError)) {
    return error instanceof Error && error.message ? error.message : fallback;
  }
  const details = isRecord(error.body.details) ? error.body.details : undefined;
  const row = typeof details?.row === "number" && details.row > 0 ? details.row : undefined;
  const fields = Array.isArray(details?.fields)
    ? details.fields.filter((field): field is string => typeof field === "string" && field.trim() !== "")
    : [];
  const location = row === undefined
    ? ""
    : fields.length > 0
      ? `第 ${row} 行（${fields.join("、")}）：`
      : `第 ${row} 行：`;
  const hint = typeof details?.hint === "string" && details.hint.trim() !== "" ? details.hint.trim() : "";
  return `${location}${error.message}${hint ? `。${hint}` : ""}`;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

type TokenGetter = () => string | null;
type TokenSetter = (token: string | null) => void;

export class ApiClient {
  private refreshPromise: Promise<string> | null = null;

  constructor(
    private getToken: TokenGetter = () => null,
    private setToken: TokenSetter = () => undefined,
    private readonly baseURL = "/api/v1",
  ) {}

  configureTokenAccess(getter: TokenGetter, setter: TokenSetter) {
    this.getToken = getter;
    this.setToken = setter;
  }

  get<T>(path: string) {
    return this.request<T>(path, { method: "GET" });
  }

  post<T>(path: string, body?: unknown) {
    return this.request<T>(path, this.jsonOptions("POST", body));
  }

  put<T>(path: string, body?: unknown) {
    return this.request<T>(path, this.jsonOptions("PUT", body));
  }

  patch<T>(path: string, body?: unknown) {
    return this.request<T>(path, this.jsonOptions("PATCH", body));
  }

  delete<T>(path: string, body?: unknown) {
    return this.request<T>(path, this.jsonOptions("DELETE", body));
  }

  upload<T>(path: string, form: FormData) {
    return this.request<T>(path, { method: "POST", body: form });
  }

  private jsonOptions(method: string, body?: unknown): RequestInit {
    return {
      method,
      headers: { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    };
  }

  private async request<T>(
    path: string,
    options: RequestInit,
    retried = false,
  ): Promise<T> {
    const headers = new Headers(options.headers);
    const token = this.getToken();
    if (token) headers.set("Authorization", `Bearer ${token}`);
    if (path === "/auth/logout") {
      const csrf = readCookie("shiftory_csrf");
      if (csrf) headers.set("X-CSRF-Token", csrf);
    }
    const response = await fetch(`${this.baseURL}${path}`, {
      ...options,
      headers,
      credentials: "include",
    });
    if (response.status === 401 && !retried && !path.startsWith("/auth/")) {
      await this.refresh();
      return this.request<T>(path, options, true);
    }
    const contentType = response.headers.get("Content-Type") ?? "";
    const payload = contentType.includes("application/json")
      ? await response.json()
      : null;
    if (!response.ok) {
      const error = (payload as { error?: ApiErrorBody } | null)?.error ?? {
        code: "HTTP_ERROR",
        message: response.statusText,
      };
      if (response.status === 401) this.setToken(null);
      throw new ApiError(response.status, error);
    }
    return (payload as { data: T }).data;
  }

  async refresh(): Promise<string> {
    if (!this.refreshPromise) {
      this.refreshPromise = (async () => {
        const headers = new Headers({ "Content-Type": "application/json" });
        const csrf = readCookie("shiftory_csrf");
        if (csrf) headers.set("X-CSRF-Token", csrf);
        const response = await fetch(`${this.baseURL}/auth/refresh`, {
          method: "POST",
          headers,
          credentials: "include",
          body: "{}",
        });
        const payload = (await response.json()) as {
          data?: { accessToken: string };
          error?: ApiErrorBody;
        };
        if (!response.ok || !payload.data?.accessToken) {
          this.setToken(null);
          throw new ApiError(
            response.status,
            payload.error ?? {
              code: "REFRESH_FAILED",
              message: "登录状态已失效",
            },
          );
        }
        this.setToken(payload.data.accessToken);
        return payload.data.accessToken;
      })().finally(() => {
        this.refreshPromise = null;
      });
    }
    return this.refreshPromise;
  }
}

function readCookie(name: string): string | null {
  const prefix = `${encodeURIComponent(name)}=`;
  const item = document.cookie
    .split(";")
    .map((part) => part.trim())
    .find((part) => part.startsWith(prefix));
  return item ? decodeURIComponent(item.slice(prefix.length)) : null;
}

export const api = new ApiClient();
