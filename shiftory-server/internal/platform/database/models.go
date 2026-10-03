// Package database 提供 Postgres 连接与 GORM 表结构同步
package database

import "time"

// 模型仅用于结构同步，业务读写继续使用参数化 SQL
// time(0) 显式表示 Postgres 时刻，避免 GORM 将 time 标签解释为 datetime
// INT 字段使用 int32，使数据库类型不受运行平台的整数位宽影响

// UsersModel 定义 users 的字段、索引和完整性约束
type UsersModel struct {
	ID                 uint64    `gorm:"column:id;type:bigint;not null;autoIncrement;primaryKey;check:ck_users_id_nonnegative,id >= 0"`
	Username           string    `gorm:"column:username;type:varchar(64);not null"`
	UsernameNormalized string    `gorm:"column:username_normalized;type:varchar(64);not null;uniqueIndex:uq_users_username_normalized,priority:1"`
	Email              string    `gorm:"column:email;type:varchar(254);not null"`
	EmailNormalized    string    `gorm:"column:email_normalized;type:varchar(254);not null;uniqueIndex:uq_users_email_normalized,priority:1"`
	DisplayName        string    `gorm:"column:display_name;type:varchar(80);not null"`
	PasswordHash       string    `gorm:"column:password_hash;type:varchar(255);not null"`
	AvatarURL          *string   `gorm:"column:avatar_url;type:varchar(1024)"`
	Status             string    `gorm:"column:status;type:varchar(16);not null;default:'ACTIVE';check:ck_users_status,status IN ('ACTIVE', 'DISABLED')"`
	PasswordChangedAt  time.Time `gorm:"column:password_changed_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP"`
	CreatedAt          time.Time `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
	UpdatedAt          time.Time `gorm:"column:updated_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
}

// TableName 返回模型对应的业务表名
func (UsersModel) TableName() string { return "users" }

// WorkspacesModel 定义 workspaces 的字段、索引和完整性约束
type WorkspacesModel struct {
	ID                   uint64      `gorm:"column:id;type:bigint;not null;autoIncrement;primaryKey;check:ck_workspaces_id_nonnegative,id >= 0"`
	Name                 string      `gorm:"column:name;type:varchar(120);not null"`
	Timezone             string      `gorm:"column:timezone;type:varchar(64);not null;default:'Asia/Shanghai'"`
	OwnerUserID          uint64      `gorm:"column:owner_user_id;type:bigint;not null;index:idx_workspaces_owner,priority:1;check:ck_workspaces_owner_user_id_nonnegative,owner_user_id >= 0"`
	CreatedBy            uint64      `gorm:"column:created_by;type:bigint;not null;check:ck_workspaces_created_by_nonnegative,created_by >= 0"`
	CreatedAt            time.Time   `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
	UpdatedAt            time.Time   `gorm:"column:updated_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
	OwnerUserIDReference *UsersModel `gorm:"foreignKey:OwnerUserID;references:ID;constraint:fk_workspaces_owner,OnDelete:NO ACTION"`
	CreatedByReference   *UsersModel `gorm:"foreignKey:CreatedBy;references:ID;constraint:fk_workspaces_creator,OnDelete:NO ACTION"`
}

// TableName 返回模型对应的业务表名
func (WorkspacesModel) TableName() string { return "workspaces" }

// WorkspaceMembersModel 定义 workspace_members 的字段、索引和完整性约束
type WorkspaceMembersModel struct {
	ID                   uint64           `gorm:"column:id;type:bigint;not null;autoIncrement;primaryKey;check:ck_workspace_members_id_nonnegative,id >= 0"`
	WorkspaceID          uint64           `gorm:"column:workspace_id;type:bigint;not null;uniqueIndex:uq_workspace_member,priority:1;check:ck_workspace_members_workspace_id_nonnegative,workspace_id >= 0"`
	UserID               uint64           `gorm:"column:user_id;type:bigint;not null;uniqueIndex:uq_workspace_member,priority:2;index:idx_workspace_members_user,priority:1;check:ck_workspace_members_user_id_nonnegative,user_id >= 0"`
	Role                 string           `gorm:"column:role;type:varchar(16);not null;check:ck_workspace_member_role,role IN ('OWNER', 'ADMIN', 'MEMBER')"`
	Status               string           `gorm:"column:status;type:varchar(16);not null;default:'ACTIVE';index:idx_workspace_members_user,priority:2;check:ck_workspace_member_status,status IN ('ACTIVE', 'DISABLED', 'REMOVED')"`
	JoinedAt             time.Time        `gorm:"column:joined_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP"`
	LeftAt               *time.Time       `gorm:"column:left_at;type:timestamptz"`
	CreatedAt            time.Time        `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
	UpdatedAt            time.Time        `gorm:"column:updated_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
	WorkspaceIDReference *WorkspacesModel `gorm:"foreignKey:WorkspaceID;references:ID;constraint:fk_workspace_members_workspace,OnDelete:NO ACTION"`
	UserIDReference      *UsersModel      `gorm:"foreignKey:UserID;references:ID;constraint:fk_workspace_members_user,OnDelete:NO ACTION"`
}

// TableName 返回模型对应的业务表名
func (WorkspaceMembersModel) TableName() string { return "workspace_members" }

// WorkspaceInvitationsModel 定义 workspace_invitations 的字段、索引和完整性约束
type WorkspaceInvitationsModel struct {
	ID                   uint64           `gorm:"column:id;type:bigint;not null;autoIncrement;primaryKey;check:ck_workspace_invitations_id_nonnegative,id >= 0"`
	WorkspaceID          uint64           `gorm:"column:workspace_id;type:bigint;not null;index:idx_workspace_invitations_email,priority:1;check:ck_workspace_invitations_workspace_id_nonnegative,workspace_id >= 0"`
	EmailNormalized      string           `gorm:"column:email_normalized;type:varchar(254);not null;index:idx_workspace_invitations_email,priority:2"`
	Role                 string           `gorm:"column:role;type:varchar(16);not null;default:'MEMBER';check:ck_workspace_invitation_role,role IN ('ADMIN', 'MEMBER')"`
	TokenHash            []byte           `gorm:"column:token_hash;type:bytea;not null;uniqueIndex:uq_workspace_invitation_token,priority:1;check:ck_workspace_invitations_token_hash_length,octet_length(token_hash) = 32"`
	Status               string           `gorm:"column:status;type:varchar(16);not null;default:'PENDING';index:idx_workspace_invitations_email,priority:3;check:ck_workspace_invitation_status,status IN ('PENDING', 'ACCEPTED', 'REVOKED', 'EXPIRED')"`
	InvitedBy            uint64           `gorm:"column:invited_by;type:bigint;not null;check:ck_workspace_invitations_invited_by_nonnegative,invited_by >= 0"`
	ExpiresAt            time.Time        `gorm:"column:expires_at;type:timestamptz;not null"`
	AcceptedBy           *uint64          `gorm:"column:accepted_by;type:bigint;check:ck_workspace_invitations_accepted_by_nonnegative,accepted_by >= 0"`
	AcceptedAt           *time.Time       `gorm:"column:accepted_at;type:timestamptz"`
	CreatedAt            time.Time        `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
	WorkspaceIDReference *WorkspacesModel `gorm:"foreignKey:WorkspaceID;references:ID;constraint:fk_workspace_invitations_workspace,OnDelete:NO ACTION"`
	InvitedByReference   *UsersModel      `gorm:"foreignKey:InvitedBy;references:ID;constraint:fk_workspace_invitations_inviter,OnDelete:NO ACTION"`
	AcceptedByReference  *UsersModel      `gorm:"foreignKey:AcceptedBy;references:ID;constraint:fk_workspace_invitations_acceptor,OnDelete:NO ACTION"`
}

// TableName 返回模型对应的业务表名
func (WorkspaceInvitationsModel) TableName() string { return "workspace_invitations" }

// ShiftsModel 定义 shifts 的字段、索引和完整性约束
type ShiftsModel struct {
	ID                   uint64           `gorm:"column:id;type:bigint;not null;autoIncrement;primaryKey;check:ck_shifts_id_nonnegative,id >= 0"`
	WorkspaceID          uint64           `gorm:"column:workspace_id;type:bigint;not null;uniqueIndex:uq_shifts_code,priority:1;index:idx_shifts_workspace_order,priority:1;check:ck_shifts_workspace_id_nonnegative,workspace_id >= 0"`
	Name                 string           `gorm:"column:name;type:varchar(80);not null"`
	Code                 string           `gorm:"column:code;type:varchar(64);not null;uniqueIndex:uq_shifts_code,priority:2"`
	StartTime            *string          `gorm:"column:start_time;type:time(0)"`
	EndTime              *string          `gorm:"column:end_time;type:time(0)"`
	CrossDay             bool             `gorm:"column:cross_day;type:boolean;not null;default:FALSE"`
	DisplayColor         string           `gorm:"column:display_color;type:varchar(16);not null;default:'#22a06b'"`
	Enabled              bool             `gorm:"column:enabled;type:boolean;not null;default:TRUE;index:idx_shifts_workspace_order,priority:2"`
	SortOrder            int32            `gorm:"column:sort_order;type:int;not null;default:0;index:idx_shifts_workspace_order,priority:3"`
	CreatedBy            uint64           `gorm:"column:created_by;type:bigint;not null;check:ck_shifts_created_by_nonnegative,created_by >= 0"`
	CreatedAt            time.Time        `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
	UpdatedAt            time.Time        `gorm:"column:updated_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
	WorkspaceIDReference *WorkspacesModel `gorm:"foreignKey:WorkspaceID;references:ID;constraint:fk_shifts_workspace,OnDelete:NO ACTION"`
	CreatedByReference   *UsersModel      `gorm:"foreignKey:CreatedBy;references:ID;constraint:fk_shifts_creator,OnDelete:NO ACTION"`
}

// TableName 返回模型对应的业务表名
func (ShiftsModel) TableName() string { return "shifts" }

// ShiftAliasesModel 定义 shift_aliases 的字段、索引和完整性约束
type ShiftAliasesModel struct {
	ID                   uint64           `gorm:"column:id;type:bigint;not null;autoIncrement;primaryKey;check:ck_shift_aliases_id_nonnegative,id >= 0"`
	WorkspaceID          uint64           `gorm:"column:workspace_id;type:bigint;not null;uniqueIndex:uq_shift_alias,priority:1;check:ck_shift_aliases_workspace_id_nonnegative,workspace_id >= 0"`
	ShiftID              uint64           `gorm:"column:shift_id;type:bigint;not null;index:idx_shift_alias_shift,priority:1;check:ck_shift_aliases_shift_id_nonnegative,shift_id >= 0"`
	Alias                string           `gorm:"column:alias;type:varchar(80);not null"`
	AliasNormalized      string           `gorm:"column:alias_normalized;type:varchar(80);not null;uniqueIndex:uq_shift_alias,priority:2"`
	CreatedAt            time.Time        `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
	WorkspaceIDReference *WorkspacesModel `gorm:"foreignKey:WorkspaceID;references:ID;constraint:fk_shift_alias_workspace,OnDelete:NO ACTION"`
	ShiftIDReference     *ShiftsModel     `gorm:"foreignKey:ShiftID;references:ID;constraint:fk_shift_alias_shift,OnDelete:NO ACTION"`
}

// TableName 返回模型对应的业务表名
func (ShiftAliasesModel) TableName() string { return "shift_aliases" }

// ImportJobsModel 定义 import_jobs 的字段、索引和完整性约束
type ImportJobsModel struct {
	RelatedImportID  *uint64    `gorm:"column:related_import_id;type:bigint;check:ck_import_jobs_related_import_id_nonnegative,related_import_id >= 0"`
	ID               uint64     `gorm:"column:id;type:bigint;not null;autoIncrement;primaryKey;check:ck_import_jobs_id_nonnegative,id >= 0"`
	WorkspaceID      uint64     `gorm:"column:workspace_id;type:bigint;not null;uniqueIndex:uq_import_job_idempotency,priority:1;index:idx_import_jobs_workspace,priority:1;check:ck_import_jobs_workspace_id_nonnegative,workspace_id >= 0"`
	UploadUserID     uint64     `gorm:"column:upload_user_id;type:bigint;not null;check:ck_import_jobs_upload_user_id_nonnegative,upload_user_id >= 0"`
	TargetUserID     uint64     `gorm:"column:target_user_id;type:bigint;not null;check:ck_import_jobs_target_user_id_nonnegative,target_user_id >= 0"`
	ImportType       string     `gorm:"column:import_type;type:varchar(16);not null;check:ck_import_job_type,import_type IN ('XLSX', 'XLS', 'IMAGE_AI', 'TEXT_AI')"`
	State            string     `gorm:"column:state;type:varchar(24);not null;default:'UPLOADED';index:idx_import_jobs_worker,priority:1;check:ck_import_job_state,state IN ('UPLOADED', 'PENDING', 'PARSING', 'NEEDS_REVIEW', 'COMMITTING', 'COMPLETED', 'FAILED', 'CANCELLED', 'ROLLED_BACK')"`
	PeriodStart      *time.Time `gorm:"column:period_start;type:date"`
	PeriodEnd        *time.Time `gorm:"column:period_end;type:date"`
	SourceFilename   string     `gorm:"column:source_filename;type:varchar(255);not null"`
	IdempotencyKey   string     `gorm:"column:idempotency_key;type:varchar(128);not null;uniqueIndex:uq_import_job_idempotency,priority:2"`
	RunGeneration    uint64     `gorm:"column:run_generation;type:bigint;not null;default:1;check:ck_import_jobs_run_generation_nonnegative,run_generation >= 0"`
	InputFingerprint string     `gorm:"column:input_fingerprint;type:varchar(64);not null;default:''"`
	ReviewVersion    uint64     `gorm:"column:review_version;type:bigint;not null;default:1;check:ck_import_jobs_review_version_nonnegative,review_version >= 0"`
	Stage            string     `gorm:"column:stage;type:varchar(32);not null;default:'WAITING'"`
	Description      *string    `gorm:"column:description;type:text"`
	InputSnapshot    []byte     `gorm:"column:input_snapshot;type:jsonb"`
	RuleSnapshot     []byte     `gorm:"column:rule_snapshot;type:jsonb"`
	JobIssues        []byte     `gorm:"column:job_issues;type:jsonb"`
	WriteCount       int        `gorm:"column:write_count;type:int;not null;default:0"`

	AttemptCount            int32            `gorm:"column:attempt_count;type:int;not null;default:0"`
	MaxAttempts             int32            `gorm:"column:max_attempts;type:int;not null;default:3"`
	LeaseOwner              *string          `gorm:"column:lease_owner;type:varchar(128)"`
	LeaseExpiresAt          *time.Time       `gorm:"column:lease_expires_at;type:timestamptz;index:idx_import_jobs_worker,priority:2"`
	HeartbeatAt             *time.Time       `gorm:"column:heartbeat_at;type:timestamptz"`
	ErrorCode               *string          `gorm:"column:error_code;type:varchar(64)"`
	ErrorMessage            *string          `gorm:"column:error_message;type:text"`
	ItemCount               int32            `gorm:"column:item_count;type:int;not null;default:0"`
	ConflictCount           int32            `gorm:"column:conflict_count;type:int;not null;default:0"`
	InvalidCount            int32            `gorm:"column:invalid_count;type:int;not null;default:0"`
	ModelName               *string          `gorm:"column:model_name;type:varchar(128)"`
	PromptVersion           *string          `gorm:"column:prompt_version;type:varchar(32)"`
	SchemaVersion           *string          `gorm:"column:schema_version;type:varchar(32)"`
	RecognitionInstructions *string          `gorm:"column:recognition_instructions;type:text"`
	MappingHints            []byte           `gorm:"column:mapping_hints;type:jsonb"`
	AIRawResponse           []byte           `gorm:"column:ai_raw_response;type:jsonb"`
	RetryNotBefore          *time.Time       `gorm:"column:retry_not_before;type:timestamptz"`
	CompletedAt             *time.Time       `gorm:"column:completed_at;type:timestamptz"`
	RolledBackAt            *time.Time       `gorm:"column:rolled_back_at;type:timestamptz"`
	CreatedAt               time.Time        `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false;index:idx_import_jobs_worker,priority:3;index:idx_import_jobs_workspace,priority:2"`
	UpdatedAt               time.Time        `gorm:"column:updated_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
	WorkspaceIDReference    *WorkspacesModel `gorm:"foreignKey:WorkspaceID;references:ID;constraint:fk_import_jobs_workspace,OnDelete:NO ACTION"`
	UploadUserIDReference   *UsersModel      `gorm:"foreignKey:UploadUserID;references:ID;constraint:fk_import_jobs_uploader,OnDelete:NO ACTION"`
	TargetUserIDReference   *UsersModel      `gorm:"foreignKey:TargetUserID;references:ID;constraint:fk_import_jobs_target,OnDelete:NO ACTION"`
}

// TableName 返回模型对应的业务表名
func (ImportJobsModel) TableName() string { return "import_jobs" }

// ImportFilesModel 定义 import_files 的字段、索引和完整性约束
type ImportFilesModel struct {
	ID                   uint64           `gorm:"column:id;type:bigint;not null;autoIncrement;primaryKey;check:ck_import_files_id_nonnegative,id >= 0"`
	ImportJobID          uint64           `gorm:"column:import_job_id;type:bigint;not null;index:idx_import_files_job,priority:1;check:ck_import_files_import_job_id_nonnegative,import_job_id >= 0"`
	StorageKey           string           `gorm:"column:storage_key;type:varchar(512);not null;uniqueIndex:uq_import_file_key,priority:1"`
	OriginalName         string           `gorm:"column:original_name;type:varchar(255);not null"`
	MediaType            string           `gorm:"column:media_type;type:varchar(128);not null"`
	ByteSize             uint64           `gorm:"column:byte_size;type:bigint;not null;check:ck_import_files_byte_size_nonnegative,byte_size >= 0"`
	Sha256               []byte           `gorm:"column:sha256;type:bytea;not null;check:ck_import_files_sha256_length,octet_length(sha256) = 32"`
	ExpiresAt            *time.Time       `gorm:"column:expires_at;type:timestamptz"`
	CreatedAt            time.Time        `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
	ImportJobIDReference *ImportJobsModel `gorm:"foreignKey:ImportJobID;references:ID;constraint:fk_import_files_job,OnDelete:NO ACTION"`
}

// TableName 返回模型对应的业务表名
func (ImportFilesModel) TableName() string { return "import_files" }

// ScheduleDaysModel 定义 schedule_days 的字段、索引和完整性约束
type ScheduleDaysModel struct {
	ID                      uint64           `gorm:"column:id;type:bigint;not null;autoIncrement;primaryKey;check:ck_schedule_days_id_nonnegative,id >= 0"`
	WorkspaceID             *uint64          `gorm:"column:workspace_id;type:bigint;index:idx_schedule_days_calendar,priority:1;check:ck_schedule_days_workspace_id_nonnegative,workspace_id >= 0"`
	UserID                  uint64           `gorm:"column:user_id;type:bigint;not null;uniqueIndex:uq_schedule_day_user_date,priority:1;index:idx_schedule_days_calendar,priority:3;check:ck_schedule_days_user_id_nonnegative,user_id >= 0"`
	WorkDate                time.Time        `gorm:"column:work_date;type:date;not null;uniqueIndex:uq_schedule_day_user_date,priority:2;index:idx_schedule_days_calendar,priority:2"`
	Status                  string           `gorm:"column:status;type:varchar(16);not null;index:idx_schedule_days_calendar,priority:4;check:ck_schedule_day_status,status IN ('WORKING', 'REST')"`
	SourceType              string           `gorm:"column:source_type;type:varchar(16);not null;default:'MANUAL';check:ck_schedule_day_source,source_type IN ('MANUAL', 'XLSX', 'XLS', 'IMAGE_AI', 'TEXT_AI')"`
	SourceImportID          *uint64          `gorm:"column:source_import_id;type:bigint;index:idx_schedule_days_source_import,priority:1;check:ck_schedule_days_source_import_id_nonnegative,source_import_id >= 0"`
	Note                    string           `gorm:"column:note;type:varchar(1000);not null;default:''"`
	Version                 uint64           `gorm:"column:version;type:bigint;not null;default:1;check:ck_schedule_days_version_nonnegative,version >= 0"`
	CreatedBy               uint64           `gorm:"column:created_by;type:bigint;not null;check:ck_schedule_days_created_by_nonnegative,created_by >= 0"`
	CreatedAt               time.Time        `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
	UpdatedAt               time.Time        `gorm:"column:updated_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
	WorkspaceIDReference    *WorkspacesModel `gorm:"foreignKey:WorkspaceID;references:ID;constraint:fk_schedule_days_workspace_source,OnDelete:SET NULL"`
	UserIDReference         *UsersModel      `gorm:"foreignKey:UserID;references:ID;constraint:fk_schedule_days_user,OnDelete:NO ACTION"`
	CreatedByReference      *UsersModel      `gorm:"foreignKey:CreatedBy;references:ID;constraint:fk_schedule_days_creator,OnDelete:NO ACTION"`
	SourceImportIDReference *ImportJobsModel `gorm:"foreignKey:SourceImportID;references:ID;constraint:fk_schedule_days_import_source,OnDelete:SET NULL"`
}

// TableName 返回模型对应的业务表名
func (ScheduleDaysModel) TableName() string { return "schedule_days" }

// ScheduleSegmentsModel 定义 schedule_segments 的字段、索引和完整性约束
type ScheduleSegmentsModel struct {
	ID                     uint64             `gorm:"column:id;type:bigint;not null;autoIncrement;primaryKey;check:ck_schedule_segments_id_nonnegative,id >= 0"`
	ScheduleDayID          uint64             `gorm:"column:schedule_day_id;type:bigint;not null;index:idx_schedule_segments_day,priority:1;check:ck_schedule_segments_schedule_day_id_nonnegative,schedule_day_id >= 0"`
	SegmentType            string             `gorm:"column:segment_type;type:varchar(16);not null;check:ck_schedule_segment_type,segment_type IN ('SHIFT', 'TIME_RANGE')"`
	ShiftID                *uint64            `gorm:"column:shift_id;type:bigint;index:idx_schedule_segments_shift,priority:1;check:ck_schedule_segments_shift_id_nonnegative,shift_id >= 0"`
	ShiftNameSnapshot      *string            `gorm:"column:shift_name_snapshot;type:varchar(80)"`
	ShiftCodeSnapshot      *string            `gorm:"column:shift_code_snapshot;type:varchar(64)"`
	StartTime              *string            `gorm:"column:start_time;type:time(0)"`
	EndTime                *string            `gorm:"column:end_time;type:time(0)"`
	CrossDay               bool               `gorm:"column:cross_day;type:boolean;not null;default:FALSE"`
	DisplayColorSnapshot   *string            `gorm:"column:display_color_snapshot;type:varchar(16)"`
	SortOrder              int32              `gorm:"column:sort_order;type:int;not null;default:0;index:idx_schedule_segments_day,priority:2"`
	OriginalLabel          *string            `gorm:"column:original_label;type:varchar(120)"`
	CreatedAt              time.Time          `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
	ScheduleDayIDReference *ScheduleDaysModel `gorm:"foreignKey:ScheduleDayID;references:ID;constraint:fk_schedule_segments_day,OnDelete:CASCADE"`
	ShiftIDReference       *ShiftsModel       `gorm:"foreignKey:ShiftID;references:ID;constraint:fk_schedule_segments_shift_source,OnDelete:SET NULL"`
}

// TableName 返回模型对应的业务表名
func (ScheduleSegmentsModel) TableName() string { return "schedule_segments" }

// ScheduleRevisionsModel 定义 schedule_revisions 的字段、索引和完整性约束
type ScheduleRevisionsModel struct {
	ID                     uint64             `gorm:"column:id;type:bigint;not null;autoIncrement;primaryKey;check:ck_schedule_revisions_id_nonnegative,id >= 0"`
	ScheduleDayID          *uint64            `gorm:"column:schedule_day_id;type:bigint;check:ck_schedule_revisions_schedule_day_id_nonnegative,schedule_day_id >= 0"`
	WorkspaceID            *uint64            `gorm:"column:workspace_id;type:bigint;index:idx_schedule_revisions_day,priority:1;check:ck_schedule_revisions_workspace_id_nonnegative,workspace_id >= 0"`
	UserID                 uint64             `gorm:"column:user_id;type:bigint;not null;index:idx_schedule_revisions_day,priority:2;check:ck_schedule_revisions_user_id_nonnegative,user_id >= 0"`
	WorkDate               time.Time          `gorm:"column:work_date;type:date;not null;index:idx_schedule_revisions_day,priority:3"`
	BeforeVersion          *uint64            `gorm:"column:before_version;type:bigint;check:ck_schedule_revisions_before_version_nonnegative,before_version >= 0"`
	AfterVersion           *uint64            `gorm:"column:after_version;type:bigint;check:ck_schedule_revisions_after_version_nonnegative,after_version >= 0"`
	BeforeSnapshot         []byte             `gorm:"column:before_snapshot;type:jsonb"`
	AfterSnapshot          []byte             `gorm:"column:after_snapshot;type:jsonb"`
	ChangeType             string             `gorm:"column:change_type;type:varchar(24);not null"`
	ImportJobID            *uint64            `gorm:"column:import_job_id;type:bigint;index:idx_schedule_revisions_import,priority:1;check:ck_schedule_revisions_import_job_id_nonnegative,import_job_id >= 0"`
	ChangedBy              uint64             `gorm:"column:changed_by;type:bigint;not null;check:ck_schedule_revisions_changed_by_nonnegative,changed_by >= 0"`
	CreatedAt              time.Time          `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false;index:idx_schedule_revisions_day,priority:4"`
	ScheduleDayIDReference *ScheduleDaysModel `gorm:"foreignKey:ScheduleDayID;references:ID;constraint:fk_schedule_revisions_day,OnDelete:SET NULL"`
	WorkspaceIDReference   *WorkspacesModel   `gorm:"foreignKey:WorkspaceID;references:ID;constraint:fk_schedule_revisions_workspace_source,OnDelete:SET NULL"`
	UserIDReference        *UsersModel        `gorm:"foreignKey:UserID;references:ID;constraint:fk_schedule_revisions_user,OnDelete:NO ACTION"`
	ImportJobIDReference   *ImportJobsModel   `gorm:"foreignKey:ImportJobID;references:ID;constraint:fk_schedule_revisions_import_source,OnDelete:SET NULL"`
	ChangedByReference     *UsersModel        `gorm:"foreignKey:ChangedBy;references:ID;constraint:fk_schedule_revisions_actor,OnDelete:NO ACTION"`
}

// TableName 返回模型对应的业务表名
func (ScheduleRevisionsModel) TableName() string { return "schedule_revisions" }

// ImportItemsModel 定义 import_items 的字段、索引和完整性约束
type ImportItemsModel struct {
	ID                          uint64             `gorm:"column:id;type:bigint;not null;autoIncrement;primaryKey;check:ck_import_items_id_nonnegative,id >= 0"`
	ImportJobID                 uint64             `gorm:"column:import_job_id;type:bigint;not null;uniqueIndex:uq_import_item_date,priority:1;index:idx_import_items_job_type,priority:1;check:ck_import_items_import_job_id_nonnegative,import_job_id >= 0"`
	WorkDate                    time.Time          `gorm:"column:work_date;type:date;not null;uniqueIndex:uq_import_item_date,priority:2"`
	ItemType                    string             `gorm:"column:item_type;type:varchar(24);not null;index:idx_import_items_job_type,priority:2;check:ck_import_item_type,item_type IN ('NEW', 'SAME', 'CONFLICT', 'INVALID', 'UNCERTAIN', 'MISSING')"`
	DraftSnapshot               []byte             `gorm:"column:draft_snapshot;type:jsonb"`
	ExistingScheduleID          *uint64            `gorm:"column:existing_schedule_id;type:bigint;check:ck_import_items_existing_schedule_id_nonnegative,existing_schedule_id >= 0"`
	ExistingVersion             *uint64            `gorm:"column:existing_version;type:bigint;check:ck_import_items_existing_version_nonnegative,existing_version >= 0"`
	Decision                    *string            `gorm:"column:decision;type:varchar(24);check:ck_import_item_decision,decision IS NULL OR decision IN ('KEEP_EXISTING', 'USE_IMPORTED', 'SKIP')"`
	Issues                      []byte             `gorm:"column:issues;type:jsonb"`
	ErrorMessage                *string            `gorm:"column:error_message;type:text"`
	SortOrder                   int32              `gorm:"column:sort_order;type:int;not null;default:0"`
	CreatedAt                   time.Time          `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
	UpdatedAt                   time.Time          `gorm:"column:updated_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
	ImportJobIDReference        *ImportJobsModel   `gorm:"foreignKey:ImportJobID;references:ID;constraint:fk_import_items_job,OnDelete:CASCADE"`
	ExistingScheduleIDReference *ScheduleDaysModel `gorm:"foreignKey:ExistingScheduleID;references:ID;constraint:fk_import_items_existing,OnDelete:SET NULL"`
}

// TableName 返回模型对应的业务表名
func (ImportItemsModel) TableName() string { return "import_items" }

// AuthRefreshTokensModel 定义 auth_refresh_tokens 的字段、索引和完整性约束
type AuthRefreshTokensModel struct {
	ID              uint64      `gorm:"column:id;type:bigint;not null;autoIncrement;primaryKey;check:ck_auth_refresh_tokens_id_nonnegative,id >= 0"`
	UserID          uint64      `gorm:"column:user_id;type:bigint;not null;index:idx_refresh_user,priority:1;check:ck_auth_refresh_tokens_user_id_nonnegative,user_id >= 0"`
	FamilyID        string      `gorm:"column:family_id;type:char(36);not null;index:idx_refresh_family,priority:1"`
	JWTID           string      `gorm:"column:jwt_id;type:char(36);not null;uniqueIndex:uq_refresh_jwt_id,priority:1"`
	TokenHash       []byte      `gorm:"column:token_hash;type:bytea;not null;uniqueIndex:uq_refresh_token_hash,priority:1;check:ck_auth_refresh_tokens_token_hash_length,octet_length(token_hash) = 32"`
	ExpiresAt       time.Time   `gorm:"column:expires_at;type:timestamptz;not null;index:idx_refresh_user,priority:2"`
	UsedAt          *time.Time  `gorm:"column:used_at;type:timestamptz"`
	RevokedAt       *time.Time  `gorm:"column:revoked_at;type:timestamptz;index:idx_refresh_family,priority:2"`
	ReplacedByJWTID *string     `gorm:"column:replaced_by_jwt_id;type:char(36)"`
	UserAgent       *string     `gorm:"column:user_agent;type:varchar(512)"`
	IPAddress       *string     `gorm:"column:ip_address;type:varchar(64)"`
	CreatedAt       time.Time   `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
	UserIDReference *UsersModel `gorm:"foreignKey:UserID;references:ID;constraint:fk_refresh_user,OnDelete:NO ACTION"`
}

// TableName 返回模型对应的业务表名
func (AuthRefreshTokensModel) TableName() string { return "auth_refresh_tokens" }

// AuditLogsModel 定义 audit_logs 的字段、索引和完整性约束
type AuditLogsModel struct {
	ID                   uint64           `gorm:"column:id;type:bigint;not null;autoIncrement;primaryKey;check:ck_audit_logs_id_nonnegative,id >= 0"`
	WorkspaceID          *uint64          `gorm:"column:workspace_id;type:bigint;index:idx_audit_workspace_time,priority:1;check:ck_audit_logs_workspace_id_nonnegative,workspace_id >= 0"`
	ActorUserID          *uint64          `gorm:"column:actor_user_id;type:bigint;index:idx_audit_actor_time,priority:1;check:ck_audit_logs_actor_user_id_nonnegative,actor_user_id >= 0"`
	Action               string           `gorm:"column:action;type:varchar(80);not null"`
	TargetType           string           `gorm:"column:target_type;type:varchar(64);not null"`
	TargetID             *string          `gorm:"column:target_id;type:varchar(128)"`
	RequestID            *string          `gorm:"column:request_id;type:varchar(64)"`
	IPAddress            *string          `gorm:"column:ip_address;type:varchar(64)"`
	UserAgent            *string          `gorm:"column:user_agent;type:varchar(512)"`
	Details              []byte           `gorm:"column:details;type:jsonb"`
	CreatedAt            time.Time        `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false;index:idx_audit_workspace_time,priority:2;index:idx_audit_actor_time,priority:2"`
	WorkspaceIDReference *WorkspacesModel `gorm:"foreignKey:WorkspaceID;references:ID;constraint:fk_audit_workspace,OnDelete:NO ACTION"`
	ActorUserIDReference *UsersModel      `gorm:"foreignKey:ActorUserID;references:ID;constraint:fk_audit_actor,OnDelete:NO ACTION"`
}

// TableName 返回模型对应的业务表名
func (AuditLogsModel) TableName() string { return "audit_logs" }

// UserPreferencesModel 定义 user_preferences 的字段、索引和完整性约束
type UserPreferencesModel struct {
	UserID                      uint64           `gorm:"column:user_id;type:bigint;not null;primaryKey;autoIncrement:false;check:ck_user_preferences_user_id_nonnegative,user_id >= 0"`
	CurrentWorkspaceID          *uint64          `gorm:"column:current_workspace_id;type:bigint;check:ck_user_preferences_current_workspace_id_nonnegative,current_workspace_id >= 0"`
	Theme                       string           `gorm:"column:theme;type:varchar(24);not null;default:'mint';check:ck_preferences_theme,theme IN ('mint', 'sky', 'lilac', 'sakura', 'amber', 'graphite')"`
	CreatedAt                   time.Time        `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
	UpdatedAt                   time.Time        `gorm:"column:updated_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false;autoUpdateTime:false"`
	UserIDReference             *UsersModel      `gorm:"foreignKey:UserID;references:ID;constraint:fk_preferences_user,OnDelete:CASCADE"`
	CurrentWorkspaceIDReference *WorkspacesModel `gorm:"foreignKey:CurrentWorkspaceID;references:ID;constraint:fk_preferences_workspace,OnDelete:SET NULL"`
}

// TableName 返回模型对应的业务表名
func (UserPreferencesModel) TableName() string { return "user_preferences" }

// schemaModels 按外键依赖返回完整业务表模型
func schemaModels() []any {
	return []any{&UsersModel{}, &WorkspacesModel{}, &WorkspaceMembersModel{}, &WorkspaceInvitationsModel{}, &ShiftsModel{}, &ShiftAliasesModel{}, &ImportJobsModel{}, &ImportOutboxModel{}, &ImportAttemptsModel{}, &ImportProviderCallsModel{}, &ImportFilesModel{}, &ScheduleDaysModel{}, &ScheduleSegmentsModel{}, &ScheduleRevisionsModel{}, &ImportItemsModel{}, &AuthRefreshTokensModel{}, &AuditLogsModel{}, &UserPreferencesModel{}}
}

type ImportOutboxModel struct {
	ID         uint64     `gorm:"column:id;type:bigint;primaryKey;autoIncrement;check:ck_import_outbox_id_nonnegative,id >= 0"`
	JobID      uint64     `gorm:"column:job_id;type:bigint;not null;uniqueIndex:uq_outbox_generation,priority:1;check:ck_import_outbox_job_id_nonnegative,job_id >= 0"`
	Generation uint64     `gorm:"column:generation;type:bigint;not null;uniqueIndex:uq_outbox_generation,priority:2;check:ck_import_outbox_generation_nonnegative,generation >= 0"`
	State      string     `gorm:"column:state;type:varchar(16);not null;default:'PENDING';index:idx_outbox_delivery,priority:1"`
	NextAt     time.Time  `gorm:"column:next_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;index:idx_outbox_delivery,priority:2"`
	Token      *string    `gorm:"column:token;type:varchar(64)"`
	LeaseUntil *time.Time `gorm:"column:lease_until;type:timestamptz"`
	ErrorCode  *string    `gorm:"column:error_code;type:varchar(64)"`
	CreatedAt  time.Time  `gorm:"column:created_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false"`
}

func (ImportOutboxModel) TableName() string { return "import_outbox" }

type ImportAttemptsModel struct {
	ID            uint64     `gorm:"column:id;type:bigint;primaryKey;autoIncrement;check:ck_import_attempts_id_nonnegative,id >= 0"`
	JobID         uint64     `gorm:"column:job_id;type:bigint;not null;uniqueIndex:uq_attempt_round,priority:1;check:ck_import_attempts_job_id_nonnegative,job_id >= 0"`
	Generation    uint64     `gorm:"column:generation;type:bigint;not null;uniqueIndex:uq_attempt_round,priority:2;check:ck_import_attempts_generation_nonnegative,generation >= 0"`
	Round         int        `gorm:"column:round;type:int;not null;uniqueIndex:uq_attempt_round,priority:3"`
	State         string     `gorm:"column:state;type:varchar(24);not null"`
	StartedAt     time.Time  `gorm:"column:started_at;type:timestamptz;not null;default:CURRENT_TIMESTAMP;autoCreateTime:false"`
	FinishedAt    *time.Time `gorm:"column:finished_at;type:timestamptz"`
	Fingerprint   string     `gorm:"column:config_fingerprint;type:varchar(64);not null;default:''"`
	PromptVersion string     `gorm:"column:prompt_version;type:varchar(32);not null;default:''"`
	SchemaVersion string     `gorm:"column:schema_version;type:varchar(64);not null;default:''"`
}

func (ImportAttemptsModel) TableName() string { return "import_attempts" }

type ImportProviderCallsModel struct {
	FinishReason string     `gorm:"column:finish_reason;type:varchar(32);not null;default:''"`
	OutputTokens int        `gorm:"column:output_tokens;type:int;not null;default:0"`
	ID           uint64     `gorm:"column:id;type:bigint;primaryKey;autoIncrement;check:ck_import_provider_calls_id_nonnegative,id >= 0"`
	AttemptID    uint64     `gorm:"column:attempt_id;type:bigint;not null;index;check:ck_import_provider_calls_attempt_id_nonnegative,attempt_id >= 0"`
	ProviderID   string     `gorm:"column:provider_id;type:varchar(64);not null"`
	Model        string     `gorm:"column:model;type:varchar(128);not null"`
	Sequence     int        `gorm:"column:sequence;type:int;not null"`
	StartedAt    time.Time  `gorm:"column:started_at;type:timestamptz;not null;autoCreateTime:false"`
	FinishedAt   *time.Time `gorm:"column:finished_at;type:timestamptz"`
	HTTPStatus   int        `gorm:"column:http_status;type:int;not null;default:0"`
	Code         string     `gorm:"column:code;type:varchar(64);not null;default:'RUNNING'"`
	Raw          *string    `gorm:"column:raw_text;type:text"`
	Normalized   []byte     `gorm:"column:normalized;type:jsonb"`
}

func (ImportProviderCallsModel) TableName() string { return "import_provider_calls" }
