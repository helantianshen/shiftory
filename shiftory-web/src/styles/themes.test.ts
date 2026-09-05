import { describe, expect, it } from "vitest";

import { sidebarThemeColor } from "./themes";

describe("theme sidebar colors", () => {
  it("uses a theme-specific menu background", () => {
    expect(sidebarThemeColor("mint")).toBe("#18352e");
    expect(sidebarThemeColor("sky")).toBe("#1d3550");
    expect(sidebarThemeColor("lilac")).toBe("#302844");
    expect(sidebarThemeColor("sakura")).toBe("#4b2938");
    expect(sidebarThemeColor("amber")).toBe("#4a351d");
    expect(sidebarThemeColor("graphite")).toBe("#29323d");
  });
});
