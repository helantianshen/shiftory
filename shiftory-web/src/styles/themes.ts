import type { ThemeName } from "@/stores/session";

const SIDEBAR_COLORS: Record<ThemeName, string> = {
  mint: "#18352e",
  sky: "#1d3550",
  lilac: "#302844",
  sakura: "#4b2938",
  amber: "#4a351d",
  graphite: "#29323d",
};

export function sidebarThemeColor(theme: ThemeName): string {
  return SIDEBAR_COLORS[theme];
}
