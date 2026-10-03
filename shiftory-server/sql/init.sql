-- 仅用于已创建的 UTF8 空数据库，初始开发账号为 admin / ShiftoryDev123!
BEGIN;

CREATE FUNCTION public.shiftory_touch_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
 BEGIN
   IF NEW IS DISTINCT FROM OLD THEN NEW.updated_at = statement_timestamp(); END IF;
   RETURN NEW;
 END;
 $$;

CREATE TABLE public.audit_logs (
    id bigint NOT NULL,
    workspace_id bigint,
    actor_user_id bigint,
    action character varying(80) NOT NULL,
    target_type character varying(64) NOT NULL,
    target_id character varying(128),
    request_id character varying(64),
    ip_address character varying(64),
    user_agent character varying(512),
    details jsonb,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_audit_logs_actor_user_id_nonnegative CHECK (actor_user_id >= 0),
    CONSTRAINT ck_audit_logs_id_nonnegative CHECK (id >= 0),
    CONSTRAINT ck_audit_logs_workspace_id_nonnegative CHECK (workspace_id >= 0)
);

CREATE SEQUENCE public.audit_logs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.audit_logs_id_seq OWNED BY public.audit_logs.id;

CREATE TABLE public.auth_refresh_tokens (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    family_id character(36) NOT NULL,
    jwt_id character(36) NOT NULL,
    token_hash bytea NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    used_at timestamp with time zone,
    revoked_at timestamp with time zone,
    replaced_by_jwt_id character(36),
    user_agent character varying(512),
    ip_address character varying(64),
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_auth_refresh_tokens_id_nonnegative CHECK (id >= 0),
    CONSTRAINT ck_auth_refresh_tokens_token_hash_length CHECK (octet_length(token_hash) = 32),
    CONSTRAINT ck_auth_refresh_tokens_user_id_nonnegative CHECK (user_id >= 0)
);

CREATE SEQUENCE public.auth_refresh_tokens_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.auth_refresh_tokens_id_seq OWNED BY public.auth_refresh_tokens.id;

CREATE TABLE public.import_attempts (
    id bigint NOT NULL,
    job_id bigint NOT NULL,
    generation bigint NOT NULL,
    round bigint NOT NULL,
    state character varying(24) NOT NULL,
    started_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    finished_at timestamp with time zone,
    config_fingerprint character varying(64) DEFAULT ''::character varying NOT NULL,
    prompt_version character varying(32) DEFAULT ''::character varying NOT NULL,
    schema_version character varying(64) DEFAULT ''::character varying NOT NULL,
    CONSTRAINT ck_import_attempts_generation_nonnegative CHECK (generation >= 0),
    CONSTRAINT ck_import_attempts_id_nonnegative CHECK (id >= 0),
    CONSTRAINT ck_import_attempts_job_id_nonnegative CHECK (job_id >= 0)
);

CREATE SEQUENCE public.import_attempts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.import_attempts_id_seq OWNED BY public.import_attempts.id;

CREATE TABLE public.import_files (
    id bigint NOT NULL,
    import_job_id bigint NOT NULL,
    storage_key character varying(512) NOT NULL,
    original_name character varying(255) NOT NULL,
    media_type character varying(128) NOT NULL,
    byte_size bigint NOT NULL,
    sha256 bytea NOT NULL,
    expires_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_import_files_byte_size_nonnegative CHECK (byte_size >= 0),
    CONSTRAINT ck_import_files_id_nonnegative CHECK (id >= 0),
    CONSTRAINT ck_import_files_import_job_id_nonnegative CHECK (import_job_id >= 0),
    CONSTRAINT ck_import_files_sha256_length CHECK (octet_length(sha256) = 32)
);

CREATE SEQUENCE public.import_files_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.import_files_id_seq OWNED BY public.import_files.id;

CREATE TABLE public.import_items (
    id bigint NOT NULL,
    import_job_id bigint NOT NULL,
    work_date date NOT NULL,
    item_type character varying(24) NOT NULL,
    draft_snapshot jsonb,
    existing_schedule_id bigint,
    existing_version bigint,
    decision character varying(24),
    issues jsonb,
    error_message text,
    sort_order integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_import_item_decision CHECK (decision IS NULL OR decision IN ('KEEP_EXISTING', 'USE_IMPORTED', 'SKIP')),
    CONSTRAINT ck_import_item_type CHECK (item_type IN ('NEW', 'SAME', 'CONFLICT', 'INVALID', 'UNCERTAIN', 'MISSING')),
    CONSTRAINT ck_import_items_existing_schedule_id_nonnegative CHECK (existing_schedule_id >= 0),
    CONSTRAINT ck_import_items_existing_version_nonnegative CHECK (existing_version >= 0),
    CONSTRAINT ck_import_items_id_nonnegative CHECK (id >= 0),
    CONSTRAINT ck_import_items_import_job_id_nonnegative CHECK (import_job_id >= 0)
);

CREATE SEQUENCE public.import_items_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.import_items_id_seq OWNED BY public.import_items.id;

CREATE TABLE public.import_jobs (
    related_import_id bigint,
    id bigint NOT NULL,
    workspace_id bigint NOT NULL,
    upload_user_id bigint NOT NULL,
    target_user_id bigint NOT NULL,
    import_type character varying(16) NOT NULL,
    state character varying(24) DEFAULT 'UPLOADED'::character varying NOT NULL,
    period_start date,
    period_end date,
    source_filename character varying(255) NOT NULL,
    idempotency_key character varying(128) NOT NULL,
    run_generation bigint DEFAULT 1 NOT NULL,
    input_fingerprint character varying(64) DEFAULT ''::character varying NOT NULL,
    review_version bigint DEFAULT 1 NOT NULL,
    stage character varying(32) DEFAULT 'WAITING'::character varying NOT NULL,
    description text,
    input_snapshot jsonb,
    rule_snapshot jsonb,
    job_issues jsonb,
    write_count bigint DEFAULT 0 NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    max_attempts integer DEFAULT 3 NOT NULL,
    lease_owner character varying(128),
    lease_expires_at timestamp with time zone,
    heartbeat_at timestamp with time zone,
    error_code character varying(64),
    error_message text,
    item_count integer DEFAULT 0 NOT NULL,
    conflict_count integer DEFAULT 0 NOT NULL,
    invalid_count integer DEFAULT 0 NOT NULL,
    model_name character varying(128),
    prompt_version character varying(32),
    schema_version character varying(32),
    recognition_instructions text,
    mapping_hints jsonb,
    ai_raw_response jsonb,
    retry_not_before timestamp with time zone,
    completed_at timestamp with time zone,
    rolled_back_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_import_job_state CHECK (state IN ('UPLOADED', 'PENDING', 'PARSING', 'NEEDS_REVIEW', 'COMMITTING', 'COMPLETED', 'FAILED', 'CANCELLED', 'ROLLED_BACK')),
    CONSTRAINT ck_import_job_type CHECK (import_type IN ('XLSX', 'XLS', 'IMAGE_AI', 'TEXT_AI')),
    CONSTRAINT ck_import_jobs_id_nonnegative CHECK (id >= 0),
    CONSTRAINT ck_import_jobs_related_import_id_nonnegative CHECK (related_import_id >= 0),
    CONSTRAINT ck_import_jobs_review_version_nonnegative CHECK (review_version >= 0),
    CONSTRAINT ck_import_jobs_run_generation_nonnegative CHECK (run_generation >= 0),
    CONSTRAINT ck_import_jobs_target_user_id_nonnegative CHECK (target_user_id >= 0),
    CONSTRAINT ck_import_jobs_upload_user_id_nonnegative CHECK (upload_user_id >= 0),
    CONSTRAINT ck_import_jobs_workspace_id_nonnegative CHECK (workspace_id >= 0)
);

CREATE SEQUENCE public.import_jobs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.import_jobs_id_seq OWNED BY public.import_jobs.id;

CREATE TABLE public.import_outbox (
    id bigint NOT NULL,
    job_id bigint NOT NULL,
    generation bigint NOT NULL,
    state character varying(16) DEFAULT 'PENDING'::character varying NOT NULL,
    next_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    token character varying(64),
    lease_until timestamp with time zone,
    error_code character varying(64),
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_import_outbox_generation_nonnegative CHECK (generation >= 0),
    CONSTRAINT ck_import_outbox_id_nonnegative CHECK (id >= 0),
    CONSTRAINT ck_import_outbox_job_id_nonnegative CHECK (job_id >= 0)
);

CREATE SEQUENCE public.import_outbox_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.import_outbox_id_seq OWNED BY public.import_outbox.id;

CREATE TABLE public.import_provider_calls (
    finish_reason character varying(32) DEFAULT ''::character varying NOT NULL,
    output_tokens bigint DEFAULT 0 NOT NULL,
    id bigint NOT NULL,
    attempt_id bigint NOT NULL,
    provider_id character varying(64) NOT NULL,
    model character varying(128) NOT NULL,
    sequence bigint NOT NULL,
    started_at timestamp with time zone NOT NULL,
    finished_at timestamp with time zone,
    http_status bigint DEFAULT 0 NOT NULL,
    code character varying(64) DEFAULT 'RUNNING'::character varying NOT NULL,
    raw_text text,
    normalized jsonb,
    CONSTRAINT ck_import_provider_calls_attempt_id_nonnegative CHECK (attempt_id >= 0),
    CONSTRAINT ck_import_provider_calls_id_nonnegative CHECK (id >= 0)
);

CREATE SEQUENCE public.import_provider_calls_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.import_provider_calls_id_seq OWNED BY public.import_provider_calls.id;

CREATE TABLE public.schedule_days (
    id bigint NOT NULL,
    workspace_id bigint,
    user_id bigint NOT NULL,
    work_date date NOT NULL,
    status character varying(16) NOT NULL,
    source_type character varying(16) DEFAULT 'MANUAL'::character varying NOT NULL,
    source_import_id bigint,
    note character varying(1000) DEFAULT ''::character varying NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_schedule_day_source CHECK (source_type IN ('MANUAL', 'XLSX', 'XLS', 'IMAGE_AI', 'TEXT_AI')),
    CONSTRAINT ck_schedule_day_status CHECK (status IN ('WORKING', 'REST')),
    CONSTRAINT ck_schedule_days_created_by_nonnegative CHECK (created_by >= 0),
    CONSTRAINT ck_schedule_days_id_nonnegative CHECK (id >= 0),
    CONSTRAINT ck_schedule_days_source_import_id_nonnegative CHECK (source_import_id >= 0),
    CONSTRAINT ck_schedule_days_user_id_nonnegative CHECK (user_id >= 0),
    CONSTRAINT ck_schedule_days_version_nonnegative CHECK (version >= 0),
    CONSTRAINT ck_schedule_days_workspace_id_nonnegative CHECK (workspace_id >= 0)
);

CREATE SEQUENCE public.schedule_days_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.schedule_days_id_seq OWNED BY public.schedule_days.id;

CREATE TABLE public.schedule_revisions (
    id bigint NOT NULL,
    schedule_day_id bigint,
    workspace_id bigint,
    user_id bigint NOT NULL,
    work_date date NOT NULL,
    before_version bigint,
    after_version bigint,
    before_snapshot jsonb,
    after_snapshot jsonb,
    change_type character varying(24) NOT NULL,
    import_job_id bigint,
    changed_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_schedule_revisions_after_version_nonnegative CHECK (after_version >= 0),
    CONSTRAINT ck_schedule_revisions_before_version_nonnegative CHECK (before_version >= 0),
    CONSTRAINT ck_schedule_revisions_changed_by_nonnegative CHECK (changed_by >= 0),
    CONSTRAINT ck_schedule_revisions_id_nonnegative CHECK (id >= 0),
    CONSTRAINT ck_schedule_revisions_import_job_id_nonnegative CHECK (import_job_id >= 0),
    CONSTRAINT ck_schedule_revisions_schedule_day_id_nonnegative CHECK (schedule_day_id >= 0),
    CONSTRAINT ck_schedule_revisions_user_id_nonnegative CHECK (user_id >= 0),
    CONSTRAINT ck_schedule_revisions_workspace_id_nonnegative CHECK (workspace_id >= 0)
);

CREATE SEQUENCE public.schedule_revisions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.schedule_revisions_id_seq OWNED BY public.schedule_revisions.id;

CREATE TABLE public.schedule_segments (
    id bigint NOT NULL,
    schedule_day_id bigint NOT NULL,
    segment_type character varying(16) NOT NULL,
    shift_id bigint,
    shift_name_snapshot character varying(80),
    shift_code_snapshot character varying(64),
    start_time time(0) without time zone,
    end_time time(0) without time zone,
    cross_day boolean DEFAULT false NOT NULL,
    display_color_snapshot character varying(16),
    sort_order integer DEFAULT 0 NOT NULL,
    original_label character varying(120),
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_schedule_segment_type CHECK (segment_type IN ('SHIFT', 'TIME_RANGE')),
    CONSTRAINT ck_schedule_segments_id_nonnegative CHECK (id >= 0),
    CONSTRAINT ck_schedule_segments_schedule_day_id_nonnegative CHECK (schedule_day_id >= 0),
    CONSTRAINT ck_schedule_segments_shift_id_nonnegative CHECK (shift_id >= 0)
);

CREATE SEQUENCE public.schedule_segments_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.schedule_segments_id_seq OWNED BY public.schedule_segments.id;

CREATE TABLE public.shift_aliases (
    id bigint NOT NULL,
    workspace_id bigint NOT NULL,
    shift_id bigint NOT NULL,
    alias character varying(80) NOT NULL,
    alias_normalized character varying(80) NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_shift_aliases_id_nonnegative CHECK (id >= 0),
    CONSTRAINT ck_shift_aliases_shift_id_nonnegative CHECK (shift_id >= 0),
    CONSTRAINT ck_shift_aliases_workspace_id_nonnegative CHECK (workspace_id >= 0)
);

CREATE SEQUENCE public.shift_aliases_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.shift_aliases_id_seq OWNED BY public.shift_aliases.id;

CREATE TABLE public.shifts (
    id bigint NOT NULL,
    workspace_id bigint NOT NULL,
    name character varying(80) NOT NULL,
    code character varying(64) NOT NULL,
    start_time time(0) without time zone,
    end_time time(0) without time zone,
    cross_day boolean DEFAULT false NOT NULL,
    display_color character varying(16) DEFAULT '#22a06b'::character varying NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_shifts_created_by_nonnegative CHECK (created_by >= 0),
    CONSTRAINT ck_shifts_id_nonnegative CHECK (id >= 0),
    CONSTRAINT ck_shifts_workspace_id_nonnegative CHECK (workspace_id >= 0)
);

CREATE SEQUENCE public.shifts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.shifts_id_seq OWNED BY public.shifts.id;

CREATE TABLE public.user_preferences (
    user_id bigint NOT NULL,
    current_workspace_id bigint,
    theme character varying(24) DEFAULT 'mint'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_preferences_theme CHECK (theme IN ('mint', 'sky', 'lilac', 'sakura', 'amber', 'graphite')),
    CONSTRAINT ck_user_preferences_current_workspace_id_nonnegative CHECK (current_workspace_id >= 0),
    CONSTRAINT ck_user_preferences_user_id_nonnegative CHECK (user_id >= 0)
);

CREATE TABLE public.users (
    id bigint NOT NULL,
    username character varying(64) NOT NULL,
    username_normalized character varying(64) NOT NULL,
    email character varying(254) NOT NULL,
    email_normalized character varying(254) NOT NULL,
    display_name character varying(80) NOT NULL,
    password_hash character varying(255) NOT NULL,
    avatar_url character varying(1024),
    status character varying(16) DEFAULT 'ACTIVE'::character varying NOT NULL,
    password_changed_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_users_id_nonnegative CHECK (id >= 0),
    CONSTRAINT ck_users_status CHECK (status IN ('ACTIVE', 'DISABLED'))
);

CREATE SEQUENCE public.users_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.users_id_seq OWNED BY public.users.id;

CREATE TABLE public.workspace_invitations (
    id bigint NOT NULL,
    workspace_id bigint NOT NULL,
    email_normalized character varying(254) NOT NULL,
    role character varying(16) DEFAULT 'MEMBER'::character varying NOT NULL,
    token_hash bytea NOT NULL,
    status character varying(16) DEFAULT 'PENDING'::character varying NOT NULL,
    invited_by bigint NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    accepted_by bigint,
    accepted_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_workspace_invitation_role CHECK (role IN ('ADMIN', 'MEMBER')),
    CONSTRAINT ck_workspace_invitation_status CHECK (status IN ('PENDING', 'ACCEPTED', 'REVOKED', 'EXPIRED')),
    CONSTRAINT ck_workspace_invitations_accepted_by_nonnegative CHECK (accepted_by >= 0),
    CONSTRAINT ck_workspace_invitations_id_nonnegative CHECK (id >= 0),
    CONSTRAINT ck_workspace_invitations_invited_by_nonnegative CHECK (invited_by >= 0),
    CONSTRAINT ck_workspace_invitations_token_hash_length CHECK (octet_length(token_hash) = 32),
    CONSTRAINT ck_workspace_invitations_workspace_id_nonnegative CHECK (workspace_id >= 0)
);

CREATE SEQUENCE public.workspace_invitations_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.workspace_invitations_id_seq OWNED BY public.workspace_invitations.id;

CREATE TABLE public.workspace_members (
    id bigint NOT NULL,
    workspace_id bigint NOT NULL,
    user_id bigint NOT NULL,
    role character varying(16) NOT NULL,
    status character varying(16) DEFAULT 'ACTIVE'::character varying NOT NULL,
    joined_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    left_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_workspace_member_role CHECK (role IN ('OWNER', 'ADMIN', 'MEMBER')),
    CONSTRAINT ck_workspace_member_status CHECK (status IN ('ACTIVE', 'DISABLED', 'REMOVED')),
    CONSTRAINT ck_workspace_members_id_nonnegative CHECK (id >= 0),
    CONSTRAINT ck_workspace_members_user_id_nonnegative CHECK (user_id >= 0),
    CONSTRAINT ck_workspace_members_workspace_id_nonnegative CHECK (workspace_id >= 0)
);

CREATE SEQUENCE public.workspace_members_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.workspace_members_id_seq OWNED BY public.workspace_members.id;

CREATE TABLE public.workspaces (
    id bigint NOT NULL,
    name character varying(120) NOT NULL,
    timezone character varying(64) DEFAULT 'Asia/Shanghai'::character varying NOT NULL,
    owner_user_id bigint NOT NULL,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_workspaces_created_by_nonnegative CHECK (created_by >= 0),
    CONSTRAINT ck_workspaces_id_nonnegative CHECK (id >= 0),
    CONSTRAINT ck_workspaces_owner_user_id_nonnegative CHECK (owner_user_id >= 0)
);

CREATE SEQUENCE public.workspaces_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.workspaces_id_seq OWNED BY public.workspaces.id;

ALTER TABLE ONLY public.audit_logs ALTER COLUMN id SET DEFAULT nextval('public.audit_logs_id_seq'::regclass);

ALTER TABLE ONLY public.auth_refresh_tokens ALTER COLUMN id SET DEFAULT nextval('public.auth_refresh_tokens_id_seq'::regclass);

ALTER TABLE ONLY public.import_attempts ALTER COLUMN id SET DEFAULT nextval('public.import_attempts_id_seq'::regclass);

ALTER TABLE ONLY public.import_files ALTER COLUMN id SET DEFAULT nextval('public.import_files_id_seq'::regclass);

ALTER TABLE ONLY public.import_items ALTER COLUMN id SET DEFAULT nextval('public.import_items_id_seq'::regclass);

ALTER TABLE ONLY public.import_jobs ALTER COLUMN id SET DEFAULT nextval('public.import_jobs_id_seq'::regclass);

ALTER TABLE ONLY public.import_outbox ALTER COLUMN id SET DEFAULT nextval('public.import_outbox_id_seq'::regclass);

ALTER TABLE ONLY public.import_provider_calls ALTER COLUMN id SET DEFAULT nextval('public.import_provider_calls_id_seq'::regclass);

ALTER TABLE ONLY public.schedule_days ALTER COLUMN id SET DEFAULT nextval('public.schedule_days_id_seq'::regclass);

ALTER TABLE ONLY public.schedule_revisions ALTER COLUMN id SET DEFAULT nextval('public.schedule_revisions_id_seq'::regclass);

ALTER TABLE ONLY public.schedule_segments ALTER COLUMN id SET DEFAULT nextval('public.schedule_segments_id_seq'::regclass);

ALTER TABLE ONLY public.shift_aliases ALTER COLUMN id SET DEFAULT nextval('public.shift_aliases_id_seq'::regclass);

ALTER TABLE ONLY public.shifts ALTER COLUMN id SET DEFAULT nextval('public.shifts_id_seq'::regclass);

ALTER TABLE ONLY public.users ALTER COLUMN id SET DEFAULT nextval('public.users_id_seq'::regclass);

ALTER TABLE ONLY public.workspace_invitations ALTER COLUMN id SET DEFAULT nextval('public.workspace_invitations_id_seq'::regclass);

ALTER TABLE ONLY public.workspace_members ALTER COLUMN id SET DEFAULT nextval('public.workspace_members_id_seq'::regclass);

ALTER TABLE ONLY public.workspaces ALTER COLUMN id SET DEFAULT nextval('public.workspaces_id_seq'::regclass);

ALTER TABLE ONLY public.audit_logs
    ADD CONSTRAINT audit_logs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.auth_refresh_tokens
    ADD CONSTRAINT auth_refresh_tokens_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.import_attempts
    ADD CONSTRAINT import_attempts_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.import_files
    ADD CONSTRAINT import_files_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.import_items
    ADD CONSTRAINT import_items_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.import_jobs
    ADD CONSTRAINT import_jobs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.import_outbox
    ADD CONSTRAINT import_outbox_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.import_provider_calls
    ADD CONSTRAINT import_provider_calls_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.schedule_days
    ADD CONSTRAINT schedule_days_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.schedule_revisions
    ADD CONSTRAINT schedule_revisions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.schedule_segments
    ADD CONSTRAINT schedule_segments_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.shift_aliases
    ADD CONSTRAINT shift_aliases_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.shifts
    ADD CONSTRAINT shifts_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.user_preferences
    ADD CONSTRAINT user_preferences_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.workspace_invitations
    ADD CONSTRAINT workspace_invitations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.workspace_members
    ADD CONSTRAINT workspace_members_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.workspaces
    ADD CONSTRAINT workspaces_pkey PRIMARY KEY (id);

CREATE INDEX idx_audit_actor_time ON public.audit_logs USING btree (actor_user_id, created_at);

CREATE INDEX idx_audit_workspace_time ON public.audit_logs USING btree (workspace_id, created_at);

CREATE INDEX idx_import_files_job ON public.import_files USING btree (import_job_id);

CREATE INDEX idx_import_items_job_type ON public.import_items USING btree (import_job_id, item_type);

CREATE INDEX idx_import_jobs_worker ON public.import_jobs USING btree (state, lease_expires_at, created_at);

CREATE INDEX idx_import_jobs_workspace ON public.import_jobs USING btree (workspace_id, created_at);

CREATE INDEX idx_import_provider_calls_attempt_id ON public.import_provider_calls USING btree (attempt_id);

CREATE INDEX idx_outbox_delivery ON public.import_outbox USING btree (state, next_at);

CREATE INDEX idx_refresh_family ON public.auth_refresh_tokens USING btree (family_id, revoked_at);

CREATE INDEX idx_refresh_user ON public.auth_refresh_tokens USING btree (user_id, expires_at);

CREATE INDEX idx_schedule_days_calendar ON public.schedule_days USING btree (workspace_id, work_date, user_id, status);

CREATE INDEX idx_schedule_days_source_import ON public.schedule_days USING btree (source_import_id);

CREATE INDEX idx_schedule_revisions_day ON public.schedule_revisions USING btree (workspace_id, user_id, work_date, created_at);

CREATE INDEX idx_schedule_revisions_import ON public.schedule_revisions USING btree (import_job_id);

CREATE INDEX idx_schedule_segments_day ON public.schedule_segments USING btree (schedule_day_id, sort_order);

CREATE INDEX idx_schedule_segments_shift ON public.schedule_segments USING btree (shift_id);

CREATE INDEX idx_shift_alias_shift ON public.shift_aliases USING btree (shift_id);

CREATE INDEX idx_shifts_workspace_order ON public.shifts USING btree (workspace_id, enabled, sort_order);

CREATE INDEX idx_workspace_invitations_email ON public.workspace_invitations USING btree (workspace_id, email_normalized, status);

CREATE INDEX idx_workspace_members_user ON public.workspace_members USING btree (user_id, status);

CREATE INDEX idx_workspaces_owner ON public.workspaces USING btree (owner_user_id);

CREATE UNIQUE INDEX uq_attempt_round ON public.import_attempts USING btree (job_id, generation, round);

CREATE UNIQUE INDEX uq_import_file_key ON public.import_files USING btree (storage_key);

CREATE UNIQUE INDEX uq_import_item_date ON public.import_items USING btree (import_job_id, work_date);

CREATE UNIQUE INDEX uq_import_job_idempotency ON public.import_jobs USING btree (workspace_id, idempotency_key);

CREATE UNIQUE INDEX uq_outbox_generation ON public.import_outbox USING btree (job_id, generation);

CREATE UNIQUE INDEX uq_refresh_jwt_id ON public.auth_refresh_tokens USING btree (jwt_id);

CREATE UNIQUE INDEX uq_refresh_token_hash ON public.auth_refresh_tokens USING btree (token_hash);

CREATE UNIQUE INDEX uq_schedule_day_user_date ON public.schedule_days USING btree (user_id, work_date);

CREATE UNIQUE INDEX uq_shift_alias ON public.shift_aliases USING btree (workspace_id, alias_normalized);

CREATE UNIQUE INDEX uq_shifts_code ON public.shifts USING btree (workspace_id, code);

CREATE UNIQUE INDEX uq_users_email_normalized ON public.users USING btree (email_normalized);

CREATE UNIQUE INDEX uq_users_username_normalized ON public.users USING btree (username_normalized);

CREATE UNIQUE INDEX uq_workspace_invitation_token ON public.workspace_invitations USING btree (token_hash);

CREATE UNIQUE INDEX uq_workspace_member ON public.workspace_members USING btree (workspace_id, user_id);

CREATE TRIGGER trg_import_items_updated_at BEFORE UPDATE ON public.import_items FOR EACH ROW EXECUTE FUNCTION public.shiftory_touch_updated_at();

CREATE TRIGGER trg_import_jobs_updated_at BEFORE UPDATE ON public.import_jobs FOR EACH ROW EXECUTE FUNCTION public.shiftory_touch_updated_at();

CREATE TRIGGER trg_schedule_days_updated_at BEFORE UPDATE ON public.schedule_days FOR EACH ROW EXECUTE FUNCTION public.shiftory_touch_updated_at();

CREATE TRIGGER trg_shifts_updated_at BEFORE UPDATE ON public.shifts FOR EACH ROW EXECUTE FUNCTION public.shiftory_touch_updated_at();

CREATE TRIGGER trg_user_preferences_updated_at BEFORE UPDATE ON public.user_preferences FOR EACH ROW EXECUTE FUNCTION public.shiftory_touch_updated_at();

CREATE TRIGGER trg_users_updated_at BEFORE UPDATE ON public.users FOR EACH ROW EXECUTE FUNCTION public.shiftory_touch_updated_at();

CREATE TRIGGER trg_workspace_members_updated_at BEFORE UPDATE ON public.workspace_members FOR EACH ROW EXECUTE FUNCTION public.shiftory_touch_updated_at();

CREATE TRIGGER trg_workspaces_updated_at BEFORE UPDATE ON public.workspaces FOR EACH ROW EXECUTE FUNCTION public.shiftory_touch_updated_at();

ALTER TABLE ONLY public.audit_logs
    ADD CONSTRAINT fk_audit_actor FOREIGN KEY (actor_user_id) REFERENCES public.users(id);

ALTER TABLE ONLY public.audit_logs
    ADD CONSTRAINT fk_audit_workspace FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id);

ALTER TABLE ONLY public.import_files
    ADD CONSTRAINT fk_import_files_job FOREIGN KEY (import_job_id) REFERENCES public.import_jobs(id);

ALTER TABLE ONLY public.import_items
    ADD CONSTRAINT fk_import_items_existing FOREIGN KEY (existing_schedule_id) REFERENCES public.schedule_days(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.import_items
    ADD CONSTRAINT fk_import_items_job FOREIGN KEY (import_job_id) REFERENCES public.import_jobs(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.import_jobs
    ADD CONSTRAINT fk_import_jobs_target FOREIGN KEY (target_user_id) REFERENCES public.users(id);

ALTER TABLE ONLY public.import_jobs
    ADD CONSTRAINT fk_import_jobs_uploader FOREIGN KEY (upload_user_id) REFERENCES public.users(id);

ALTER TABLE ONLY public.import_jobs
    ADD CONSTRAINT fk_import_jobs_workspace FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id);

ALTER TABLE ONLY public.user_preferences
    ADD CONSTRAINT fk_preferences_user FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.user_preferences
    ADD CONSTRAINT fk_preferences_workspace FOREIGN KEY (current_workspace_id) REFERENCES public.workspaces(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.auth_refresh_tokens
    ADD CONSTRAINT fk_refresh_user FOREIGN KEY (user_id) REFERENCES public.users(id);

ALTER TABLE ONLY public.schedule_days
    ADD CONSTRAINT fk_schedule_days_creator FOREIGN KEY (created_by) REFERENCES public.users(id);

ALTER TABLE ONLY public.schedule_days
    ADD CONSTRAINT fk_schedule_days_import_source FOREIGN KEY (source_import_id) REFERENCES public.import_jobs(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.schedule_days
    ADD CONSTRAINT fk_schedule_days_user FOREIGN KEY (user_id) REFERENCES public.users(id);

ALTER TABLE ONLY public.schedule_days
    ADD CONSTRAINT fk_schedule_days_workspace_source FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.schedule_revisions
    ADD CONSTRAINT fk_schedule_revisions_actor FOREIGN KEY (changed_by) REFERENCES public.users(id);

ALTER TABLE ONLY public.schedule_revisions
    ADD CONSTRAINT fk_schedule_revisions_day FOREIGN KEY (schedule_day_id) REFERENCES public.schedule_days(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.schedule_revisions
    ADD CONSTRAINT fk_schedule_revisions_import_source FOREIGN KEY (import_job_id) REFERENCES public.import_jobs(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.schedule_revisions
    ADD CONSTRAINT fk_schedule_revisions_user FOREIGN KEY (user_id) REFERENCES public.users(id);

ALTER TABLE ONLY public.schedule_revisions
    ADD CONSTRAINT fk_schedule_revisions_workspace_source FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.schedule_segments
    ADD CONSTRAINT fk_schedule_segments_day FOREIGN KEY (schedule_day_id) REFERENCES public.schedule_days(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.schedule_segments
    ADD CONSTRAINT fk_schedule_segments_shift_source FOREIGN KEY (shift_id) REFERENCES public.shifts(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.shift_aliases
    ADD CONSTRAINT fk_shift_alias_shift FOREIGN KEY (shift_id) REFERENCES public.shifts(id);

ALTER TABLE ONLY public.shift_aliases
    ADD CONSTRAINT fk_shift_alias_workspace FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id);

ALTER TABLE ONLY public.shifts
    ADD CONSTRAINT fk_shifts_creator FOREIGN KEY (created_by) REFERENCES public.users(id);

ALTER TABLE ONLY public.shifts
    ADD CONSTRAINT fk_shifts_workspace FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id);

ALTER TABLE ONLY public.workspace_invitations
    ADD CONSTRAINT fk_workspace_invitations_acceptor FOREIGN KEY (accepted_by) REFERENCES public.users(id);

ALTER TABLE ONLY public.workspace_invitations
    ADD CONSTRAINT fk_workspace_invitations_inviter FOREIGN KEY (invited_by) REFERENCES public.users(id);

ALTER TABLE ONLY public.workspace_invitations
    ADD CONSTRAINT fk_workspace_invitations_workspace FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id);

ALTER TABLE ONLY public.workspace_members
    ADD CONSTRAINT fk_workspace_members_user FOREIGN KEY (user_id) REFERENCES public.users(id);

ALTER TABLE ONLY public.workspace_members
    ADD CONSTRAINT fk_workspace_members_workspace FOREIGN KEY (workspace_id) REFERENCES public.workspaces(id);

ALTER TABLE ONLY public.workspaces
    ADD CONSTRAINT fk_workspaces_creator FOREIGN KEY (created_by) REFERENCES public.users(id);

ALTER TABLE ONLY public.workspaces
    ADD CONSTRAINT fk_workspaces_owner FOREIGN KEY (owner_user_id) REFERENCES public.users(id);
INSERT INTO users (id, username, username_normalized, email, email_normalized, display_name, password_hash)
VALUES (1, 'admin', 'admin', 'admin@example.com', 'admin@example.com', '开发管理员', '$argon2id$v=19$m=19456,t=2,p=1$Mqm7kgH6ZZhVwCUkXSjStA$aFd+t/soS7lcA2ePOh5weGrhrP0/vjP5l083rcs449Y');
INSERT INTO workspaces (id, name, owner_user_id, created_by) VALUES (1, '开发工作区', 1, 1);
INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (1, 1, 'OWNER');
INSERT INTO user_preferences (user_id, current_workspace_id, theme) VALUES (1, 1, 'mint');
INSERT INTO shifts (workspace_id, name, code, start_time, end_time, cross_day, display_color, sort_order, created_by) VALUES
(1, '早班', 'MORNING', '08:00:00', '16:00:00', FALSE, '#22a06b', 0, 1),
(1, '中班', 'MIDDLE', '16:00:00', '00:00:00', TRUE, '#3b82f6', 1, 1),
(1, '晚班', 'NIGHT', '00:00:00', '08:00:00', FALSE, '#8b5cf6', 2, 1);

SELECT setval(pg_get_serial_sequence('users','id'), (SELECT MAX(id) FROM users), true);
SELECT setval(pg_get_serial_sequence('workspaces','id'), (SELECT MAX(id) FROM workspaces), true);
COMMIT;
