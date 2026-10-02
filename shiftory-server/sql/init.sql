-- 创建并选择开发数据库，表结构和初始数据仅供空库初始化
CREATE DATABASE IF NOT EXISTS `shiftory` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs;
USE `shiftory`;
SET NAMES utf8mb4 COLLATE utf8mb4_0900_as_cs;

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_as_cs;

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_as_cs;

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_as_cs;

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_as_cs;

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_as_cs;

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_as_cs;

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
    recognition_instructions TEXT NULL,
    mapping_hints JSON NULL,
    ai_raw_response JSON NULL,
    retry_not_before DATETIME(6) NULL,
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_as_cs;

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_as_cs;

CREATE TABLE schedule_days (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id BIGINT UNSIGNED NULL,
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
    UNIQUE KEY uq_schedule_day_user_date (user_id, work_date),
    KEY idx_schedule_days_calendar (workspace_id, work_date, user_id, status),
    KEY idx_schedule_days_source_import (source_import_id),
    CONSTRAINT fk_schedule_days_workspace_source FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE SET NULL,
    CONSTRAINT fk_schedule_days_user FOREIGN KEY (user_id) REFERENCES users(id),
    CONSTRAINT fk_schedule_days_creator FOREIGN KEY (created_by) REFERENCES users(id),
    CONSTRAINT fk_schedule_days_import_source FOREIGN KEY (source_import_id) REFERENCES import_jobs(id) ON DELETE SET NULL,
    CONSTRAINT ck_schedule_day_status CHECK (status IN ('WORKING', 'REST')),
    CONSTRAINT ck_schedule_day_source CHECK (source_type IN ('MANUAL', 'XLSX', 'XLS', 'IMAGE_AI'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_as_cs;

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
    CONSTRAINT fk_schedule_segments_shift_source FOREIGN KEY (shift_id) REFERENCES shifts(id) ON DELETE SET NULL,
    CONSTRAINT ck_schedule_segment_type CHECK (segment_type IN ('SHIFT', 'TIME_RANGE'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_as_cs;

CREATE TABLE schedule_revisions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    schedule_day_id BIGINT UNSIGNED NULL,
    workspace_id BIGINT UNSIGNED NULL,
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
    CONSTRAINT fk_schedule_revisions_workspace_source FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE SET NULL,
    CONSTRAINT fk_schedule_revisions_user FOREIGN KEY (user_id) REFERENCES users(id),
    CONSTRAINT fk_schedule_revisions_import_source FOREIGN KEY (import_job_id) REFERENCES import_jobs(id) ON DELETE SET NULL,
    CONSTRAINT fk_schedule_revisions_actor FOREIGN KEY (changed_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_as_cs;

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_as_cs;

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_as_cs;

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_as_cs;

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_as_cs;


-- 开发初始账号为 admin，密码为 ShiftoryDev123!，密码摘要使用应用的 Argon2id 参数
-- 工作区所有者是工作区级角色，项目不存在全局管理员角色
START TRANSACTION;
INSERT INTO users (id, username, username_normalized, email, email_normalized, display_name, password_hash)
VALUES (1, 'admin', 'admin', 'admin@example.com', 'admin@example.com', '开发管理员', '$argon2id$v=19$m=19456,t=2,p=1$Mqm7kgH6ZZhVwCUkXSjStA$aFd+t/soS7lcA2ePOh5weGrhrP0/vjP5l083rcs449Y');
INSERT INTO workspaces (id, name, owner_user_id, created_by) VALUES (1, '开发工作区', 1, 1);
INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (1, 1, 'OWNER');
INSERT INTO user_preferences (user_id, current_workspace_id, theme) VALUES (1, 1, 'mint');
INSERT INTO shifts (workspace_id, name, code, start_time, end_time, cross_day, display_color, sort_order, created_by) VALUES
(1, '早班', 'MORNING', '08:00:00', '16:00:00', FALSE, '#22a06b', 0, 1),
(1, '中班', 'MIDDLE', '16:00:00', '00:00:00', TRUE, '#3b82f6', 1, 1),
(1, '晚班', 'NIGHT', '00:00:00', '08:00:00', FALSE, '#8b5cf6', 2, 1);
COMMIT;
