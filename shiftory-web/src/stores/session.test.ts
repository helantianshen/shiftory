import { beforeEach, describe, expect, it } from "vitest";
import { createPinia, setActivePinia } from "pinia";

import { useSessionStore } from "./session";

describe("session store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    localStorage.clear();
    sessionStorage.clear();
  });

  it("holds profile, workspace, theme and access token only in memory", () => {
    const store = useSessionStore();
    store.setSession(
      {
        id: 1,
        username: "alice",
        email: "alice@example.com",
        displayName: "Alice",
        avatarUrl: "",
        status: "ACTIVE",
      },
      "access.jwt.value",
    );
    store.setWorkspaces([
      { id: 7, name: "护理一组", timezone: "Asia/Shanghai", role: "OWNER" },
    ]);
    store.applyPreferences({ currentWorkspaceId: 7, theme: "lilac" });

    expect(store.user?.displayName).toBe("Alice");
    expect(store.currentWorkspace?.id).toBe(7);
    expect(store.theme).toBe("lilac");
    expect(store.accessToken).toBe("access.jwt.value");
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
  });

  it("clears authentication state on logout without discarding visual preferences", () => {
    const store = useSessionStore();
    store.setSession(
      {
        id: 1,
        username: "alice",
        email: "alice@example.com",
        displayName: "Alice",
        avatarUrl: "",
        status: "ACTIVE",
      },
      "token",
    );
    store.applyPreferences({ currentWorkspaceId: null, theme: "sky" });
    store.clearSession();
    expect(store.user).toBeNull();
    expect(store.accessToken).toBeNull();
    expect(store.theme).toBe("sky");
  });
});
