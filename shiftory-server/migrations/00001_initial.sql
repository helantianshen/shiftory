-- +goose Up
CREATE TABLE users (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    username VARCHAR(64) NOT NULL,
    username_normalized VARCHAR(64) NOT NULL,
    email VARCHAR(254) NOT NULL,
    email_normalized VARCHAR(254) NOT NULL,
    display_name VARCHAR(80) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    avatar_url VARCHAR(1024) NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
    password_changed_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_users_username_normalized (username_normalized),
    UNIQUE KEY uq_users_email_normalized (email_normalized),
    CONSTRAINT ck_users_status CHECK (status IN ('ACTIVE', 'DISABLED'))
) ENGINE=InnoDB;

CREATE TABLE workspaces (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name VARCHAR(120) NOT NULL,
    timezone VARCHAR(64) NOT NULL DEFAULT 'Asia/Shanghai',
    owner_user_id BIGINT UNSIGNED NOT NULL,
    created_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_workspaces_owner (owner_user_id),
    CONSTRAINT fk_workspaces_owner FOREIGN KEY (owner_user_id) REFERENCES users(id),
    CONSTRAINT fk_workspaces_creator FOREIGN KEY (created_by) REFERENCES users(id)
) ENGINE=InnoDB;

CREATE TABLE workspace_members (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id BIGINT UNSIGNED NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    role VARCHAR(16) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
    joined_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    left_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_workspace_member (workspace_id, user_id),
    KEY idx_workspace_members_user (user_id, status),
    CONSTRAINT fk_workspace_members_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id),
    CONSTRAINT fk_workspace_members_user FOREIGN KEY (user_id) REFERENCES users(id),
    CONSTRAINT ck_workspace_member_role CHECK (role IN ('OWNER', 'ADMIN', 'MEMBER')),
    CONSTRAINT ck_workspace_member_status CHECK (status IN ('ACTIVE', 'DISABLED', 'REMOVED'))
) ENGINE=InnoDB;

CREATE TABLE workspace_invitations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id BIGINT UNSIGNED NOT NULL,
    email_normalized VARCHAR(254) NOT NULL,
    role VARCHAR(16) NOT NULL DEFAULT 'MEMBER',
    token_hash BINARY(32) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'PENDING',
    invited_by BIGINT UNSIGNED NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    accepted_by BIGINT UNSIGNED NULL,
    accepted_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_workspace_invitation_token (token_hash),
    KEY idx_workspace_invitations_email (workspace_id, email_normalized, status),
    CONSTRAINT fk_workspace_invitations_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id),
    CONSTRAINT fk_workspace_invitations_inviter FOREIGN KEY (invited_by) REFERENCES users(id),
    CONSTRAINT fk_workspace_invitations_acceptor FOREIGN KEY (accepted_by) REFERENCES users(id),
    CONSTRAINT ck_workspace_invitation_role CHECK (role IN ('ADMIN', 'MEMBER')),
    CONSTRAINT ck_workspace_invitation_status CHECK (status IN ('PENDING', 'ACCEPTED', 'REVOKED', 'EXPIRED'))
) ENGINE=InnoDB;

CREATE TABLE shifts (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(80) NOT NULL,
    code VARCHAR(64) NOT NULL,
    start_time TIME NULL,
    end_time TIME NULL,
    cross_day BOOLEAN NOT NULL DEFAULT FALSE,
    display_color VARCHAR(16) NOT NULL DEFAULT '#22a06b',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INT NOT NULL DEFAULT 0,
    created_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_shifts_code (workspace_id, code),
    KEY idx_shifts_workspace_order (workspace_id, enabled, sort_order),
    CONSTRAINT fk_shifts_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id),
    CONSTRAINT fk_shifts_creator FOREIGN KEY (created_by) REFERENCES users(id)
) ENGINE=InnoDB;

CREATE TABLE shift_aliases (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id BIGINT UNSIGNED NOT NULL,
    shift_id BIGINT UNSIGNED NOT NULL,
    alias VARCHAR(80) NOT NULL,
    alias_normalized VARCHAR(80) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_shift_alias (workspace_id, alias_normalized),
    KEY idx_shift_alias_shift (shift_id),
    CONSTRAINT fk_shift_alias_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id),
    CONSTRAINT fk_shift_alias_shift FOREIGN KEY (shift_id) REFERENCES shifts(id)
) ENGINE=InnoDB;

CREATE TABLE import_jobs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id BIGINT UNSIGNED NOT NULL,
    upload_user_id BIGINT UNSIGNED NOT NULL,
    target_user_id BIGINT UNSIGNED NOT NULL,
    import_type VARCHAR(16) NOT NULL,
    state VARCHAR(24) NOT NULL DEFAULT 'UPLOADED',
    period_start DATE NULL,
    period_end DATE NULL,
    source_filename VARCHAR(255) NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    attempt_count INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 3,
    lease_owner VARCHAR(128) NULL,
    lease_expires_at DATETIME(6) NULL,
    heartbeat_at DATETIME(6) NULL,
    error_code VARCHAR(64) NULL,
    error_message TEXT NULL,
    item_count INT NOT NULL DEFAULT 0,
    conflict_count INT NOT NULL DEFAULT 0,
    invalid_count INT NOT NULL DEFAULT 0,
    model_name VARCHAR(128) NULL,
    prompt_version VARCHAR(32) NULL,
    schema_version VARCHAR(32) NULL,
    completed_at DATETIME(6) NULL,
    rolled_back_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_import_job_idempotency (workspace_id, idempotency_key),
    KEY idx_import_jobs_worker (state, lease_expires_at, created_at),
    KEY idx_import_jobs_workspace (workspace_id, created_at),
    CONSTRAINT fk_import_jobs_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id),
    CONSTRAINT fk_import_jobs_uploader FOREIGN KEY (upload_user_id) REFERENCES users(id),
    CONSTRAINT fk_import_jobs_target FOREIGN KEY (target_user_id) REFERENCES users(id),
    CONSTRAINT ck_import_job_type CHECK (import_type IN ('XLSX', 'XLS', 'IMAGE_AI')),
    CONSTRAINT ck_import_job_state CHECK (state IN ('UPLOADED', 'PENDING', 'PARSING', 'NEEDS_REVIEW', 'COMMITTING', 'COMPLETED', 'FAILED', 'CANCELLED', 'ROLLED_BACK'))
) ENGINE=InnoDB;

CREATE TABLE import_files (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    import_job_id BIGINT UNSIGNED NOT NULL,
    storage_key VARCHAR(512) NOT NULL,
    original_name VARCHAR(255) NOT NULL,
    media_type VARCHAR(128) NOT NULL,
    byte_size BIGINT UNSIGNED NOT NULL,
    sha256 BINARY(32) NOT NULL,
    expires_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_import_file_key (storage_key),
    KEY idx_import_files_job (import_job_id),
    CONSTRAINT fk_import_files_job FOREIGN KEY (import_job_id) REFERENCES import_jobs(id)
) ENGINE=InnoDB;

CREATE TABLE schedule_days (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id BIGINT UNSIGNED NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    work_date DATE NOT NULL,
    status VARCHAR(16) NOT NULL,
    source_type VARCHAR(16) NOT NULL DEFAULT 'MANUAL',
    source_import_id BIGINT UNSIGNED NULL,
    note VARCHAR(1000) NOT NULL DEFAULT '',
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_schedule_day (workspace_id, user_id, work_date),
    KEY idx_schedule_days_calendar (workspace_id, work_date, user_id, status),
    KEY idx_schedule_days_source_import (source_import_id),
    CONSTRAINT fk_schedule_days_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id),
    CONSTRAINT fk_schedule_days_user FOREIGN KEY (user_id) REFERENCES users(id),
    CONSTRAINT fk_schedule_days_creator FOREIGN KEY (created_by) REFERENCES users(id),
    CONSTRAINT fk_schedule_days_import FOREIGN KEY (source_import_id) REFERENCES import_jobs(id),
    CONSTRAINT ck_schedule_day_status CHECK (status IN ('WORKING', 'REST')),
    CONSTRAINT ck_schedule_day_source CHECK (source_type IN ('MANUAL', 'XLSX', 'XLS', 'IMAGE_AI'))
) ENGINE=InnoDB;

CREATE TABLE schedule_segments (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    schedule_day_id BIGINT UNSIGNED NOT NULL,
    segment_type VARCHAR(16) NOT NULL,
    shift_id BIGINT UNSIGNED NULL,
    shift_name_snapshot VARCHAR(80) NULL,
    shift_code_snapshot VARCHAR(64) NULL,
    start_time TIME NULL,
    end_time TIME NULL,
    cross_day BOOLEAN NOT NULL DEFAULT FALSE,
    display_color_snapshot VARCHAR(16) NULL,
    sort_order INT NOT NULL DEFAULT 0,
    original_label VARCHAR(120) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_schedule_segments_day (schedule_day_id, sort_order),
    KEY idx_schedule_segments_shift (shift_id),
    CONSTRAINT fk_schedule_segments_day FOREIGN KEY (schedule_day_id) REFERENCES schedule_days(id) ON DELETE CASCADE,
    CONSTRAINT fk_schedule_segments_shift FOREIGN KEY (shift_id) REFERENCES shifts(id),
    CONSTRAINT ck_schedule_segment_type CHECK (segment_type IN ('SHIFT', 'TIME_RANGE'))
) ENGINE=InnoDB;

CREATE TABLE schedule_revisions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    schedule_day_id BIGINT UNSIGNED NULL,
    workspace_id BIGINT UNSIGNED NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    work_date DATE NOT NULL,
    before_version BIGINT UNSIGNED NULL,
    after_version BIGINT UNSIGNED NULL,
    before_snapshot JSON NULL,
    after_snapshot JSON NULL,
    change_type VARCHAR(24) NOT NULL,
    import_job_id BIGINT UNSIGNED NULL,
    changed_by BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_schedule_revisions_day (workspace_id, user_id, work_date, created_at),
    KEY idx_schedule_revisions_import (import_job_id),
    CONSTRAINT fk_schedule_revisions_day FOREIGN KEY (schedule_day_id) REFERENCES schedule_days(id) ON DELETE SET NULL,
    CONSTRAINT fk_schedule_revisions_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id),
    CONSTRAINT fk_schedule_revisions_user FOREIGN KEY (user_id) REFERENCES users(id),
    CONSTRAINT fk_schedule_revisions_import FOREIGN KEY (import_job_id) REFERENCES import_jobs(id),
    CONSTRAINT fk_schedule_revisions_actor FOREIGN KEY (changed_by) REFERENCES users(id)
) ENGINE=InnoDB;

CREATE TABLE import_items (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    import_job_id BIGINT UNSIGNED NOT NULL,
    work_date DATE NOT NULL,
    item_type VARCHAR(24) NOT NULL,
    draft_snapshot JSON NULL,
    existing_schedule_id BIGINT UNSIGNED NULL,
    existing_version BIGINT UNSIGNED NULL,
    decision VARCHAR(24) NULL,
    issues JSON NULL,
    error_message TEXT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_import_item_date (import_job_id, work_date),
    KEY idx_import_items_job_type (import_job_id, item_type),
    CONSTRAINT fk_import_items_job FOREIGN KEY (import_job_id) REFERENCES import_jobs(id) ON DELETE CASCADE,
    CONSTRAINT fk_import_items_existing FOREIGN KEY (existing_schedule_id) REFERENCES schedule_days(id) ON DELETE SET NULL,
    CONSTRAINT ck_import_item_type CHECK (item_type IN ('NEW', 'SAME', 'CONFLICT', 'INVALID', 'UNCERTAIN', 'MISSING')),
    CONSTRAINT ck_import_item_decision CHECK (decision IS NULL OR decision IN ('KEEP_EXISTING', 'USE_IMPORTED', 'SKIP'))
) ENGINE=InnoDB;

CREATE TABLE auth_refresh_tokens (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    family_id CHAR(36) NOT NULL,
    jwt_id CHAR(36) NOT NULL,
    token_hash BINARY(32) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    used_at DATETIME(6) NULL,
    revoked_at DATETIME(6) NULL,
    replaced_by_jwt_id CHAR(36) NULL,
    user_agent VARCHAR(512) NULL,
    ip_address VARCHAR(64) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_refresh_token_hash (token_hash),
    UNIQUE KEY uq_refresh_jwt_id (jwt_id),
    KEY idx_refresh_family (family_id, revoked_at),
    KEY idx_refresh_user (user_id, expires_at),
    CONSTRAINT fk_refresh_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB;

CREATE TABLE audit_logs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id BIGINT UNSIGNED NULL,
    actor_user_id BIGINT UNSIGNED NULL,
    action VARCHAR(80) NOT NULL,
    target_type VARCHAR(64) NOT NULL,
    target_id VARCHAR(128) NULL,
    request_id VARCHAR(64) NULL,
    ip_address VARCHAR(64) NULL,
    user_agent VARCHAR(512) NULL,
    details JSON NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_audit_workspace_time (workspace_id, created_at),
    KEY idx_audit_actor_time (actor_user_id, created_at),
    CONSTRAINT fk_audit_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id),
    CONSTRAINT fk_audit_actor FOREIGN KEY (actor_user_id) REFERENCES users(id)
) ENGINE=InnoDB;

CREATE TABLE user_preferences (
    user_id BIGINT UNSIGNED NOT NULL,
    current_workspace_id BIGINT UNSIGNED NULL,
    theme VARCHAR(24) NOT NULL DEFAULT 'mint',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (user_id),
    CONSTRAINT fk_preferences_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_preferences_workspace FOREIGN KEY (current_workspace_id) REFERENCES workspaces(id) ON DELETE SET NULL,
    CONSTRAINT ck_preferences_theme CHECK (theme IN ('mint', 'sky', 'lilac', 'sakura', 'amber', 'graphite'))
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE user_preferences;
DROP TABLE audit_logs;
DROP TABLE auth_refresh_tokens;
DROP TABLE import_items;
DROP TABLE schedule_revisions;
DROP TABLE schedule_segments;
DROP TABLE schedule_days;
DROP TABLE import_files;
DROP TABLE import_jobs;
DROP TABLE shift_aliases;
DROP TABLE shifts;
DROP TABLE workspace_invitations;
DROP TABLE workspace_members;
DROP TABLE workspaces;
DROP TABLE users;
