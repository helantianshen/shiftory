export interface Shift {
  id: number;
  name: string;
  code: string;
  startTime?: string;
  endTime?: string;
  crossDay: boolean;
  displayColor: string;
  enabled: boolean;
  sortOrder: number;
  aliases: string[];
}

export interface ScheduleSegment {
  id?: number;
  type: "SHIFT" | "TIME_RANGE";
  shiftId?: number;
  shiftName?: string;
  shiftCode?: string;
  startTime?: string;
  endTime?: string;
  crossDay: boolean;
  displayColor?: string;
  sortOrder: number;
  originalLabel?: string;
}

export interface ScheduleDay {
  id: number;
  workspaceId: number;
  userId: number;
  workDate: string;
  status: "WORKING" | "REST";
  sourceType: "MANUAL" | "XLSX" | "XLS" | "IMAGE_AI";
  sourceImportId?: number | null;
  note: string;
  version: number;
  segments: ScheduleSegment[];
}

export interface Member {
  id: number;
  username: string;
  email: string;
  displayName: string;
  avatarUrl: string;
  role: "OWNER" | "ADMIN" | "MEMBER";
  status: "ACTIVE" | "DISABLED" | "REMOVED";
  joinedAt: string;
  leftAt?: string;
  scheduleCompleteness?: number;
  lastImportAt?: string;
}

export interface CalendarMemberDay {
  userId: number;
  status: "WORKING" | "REST" | "MISSING";
  note: string;
  sourceType?: ScheduleDay["sourceType"];
  sourceImportId?: number | null;
  version: number;
  segments: ScheduleSegment[];
}

export interface CalendarDay {
  date: string;
  working: number;
  rest: number;
  missing: number;
  allRest: boolean;
  members: CalendarMemberDay[];
}

export interface ImportItem {
  id: number;
  workDate: string;
  type: "NEW" | "SAME" | "CONFLICT" | "INVALID" | "UNCERTAIN" | "MISSING";
  decision?: "KEEP_EXISTING" | "USE_IMPORTED" | "SKIP" | "";
  draft?: { status: string; note: string; segments: ScheduleSegment[] };
  existingScheduleId?: number;
  existingVersion?: number;
  issues?: Array<string | { field?: string; message?: string }>;
  errorMessage?: string;
}

export interface ImportJob {
  id: number;
  targetUserId: number;
  importType: "XLSX" | "XLS" | "IMAGE_AI";
  state: string;
  periodStart: string;
  periodEnd: string;
  sourceFilename: string;
  itemCount: number;
  conflictCount: number;
  invalidCount: number;
  createdAt: string;
  completedAt?: string;
  rolledBackAt?: string;
  items?: ImportItem[];
}
