CREATE TABLE `task_definition` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `kind` VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `status` VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'draft',
    `verification_mode` VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `title` VARCHAR(128) NOT NULL,
    `description` VARCHAR(2048) NOT NULL,
    `base_points` BIGINT NOT NULL,
    `channel_id` BIGINT NOT NULL,
    `channel_url` VARCHAR(512) NOT NULL DEFAULT '',
    `starts_at` DATETIME(6) NOT NULL,
    `ends_at` DATETIME(6) NOT NULL,
    `revision` BIGINT NOT NULL DEFAULT 1,
    `create_key` VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NULL,
    `create_hash` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_task_create` (`create_key`),
    KEY `ix_task_catalog` (`verification_mode`, `status`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- Only committed completion facts belong here; verification previews never insert.
CREATE TABLE `task_completion` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `task_id` BIGINT NOT NULL,
    `telegram_identity_id` BIGINT NOT NULL,
    `user_id` BIGINT NOT NULL,
    `request_key` VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `task_revision` BIGINT NOT NULL,
    `base_points` BIGINT NOT NULL,
    `mode` VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'self',
    `participant_stage` VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'first',
    `invitation_id` BIGINT NULL,
    `identity_revision` BIGINT NOT NULL DEFAULT 0,
    `binding_version` BIGINT NOT NULL DEFAULT 0,
    `member_status` VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    `task_title` VARCHAR(128) NOT NULL DEFAULT '',
    `rule_version` BIGINT NOT NULL DEFAULT 0,
    `verified_at` DATETIME(6) NOT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_task_identity` (`task_id`, `telegram_identity_id`),
    UNIQUE KEY `uk_task_request` (`user_id`, `request_key`),
    KEY `fk_task_completion_identity` (`telegram_identity_id`),
    KEY `ix_completion_user` (`user_id`, `id`),
    KEY `ix_completion_user_task` (`user_id`, `task_id`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `user_reward_state` (
    `user_id` BIGINT NOT NULL,
    `first_completion_id` BIGINT NULL,
    `pro_progress` DECIMAL(36,18) NOT NULL DEFAULT 0,
    `pro_unlocked_at` DATETIME(6) NULL,
    PRIMARY KEY (`user_id`),
    UNIQUE KEY `uk_reward_first_completion` (`first_completion_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `task_invitation` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `task_id` BIGINT NOT NULL,
    `inviter_user_id` BIGINT NOT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_task_inviter` (`task_id`, `inviter_user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `user_referral` (
    `user_id` BIGINT NOT NULL,
    `parent_user_id` BIGINT NOT NULL,
    `completion_id` BIGINT NOT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`user_id`),
    UNIQUE KEY `uk_referral_completion` (`completion_id`),
    KEY `ix_referral_parent` (`parent_user_id`, `user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `reward_grant` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `completion_id` BIGINT NOT NULL,
    `user_id` BIGINT NOT NULL,
    `reason` VARCHAR(24) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `reward_usd` DECIMAL(36,18) NOT NULL,
    `points` DECIMAL(36,18) NOT NULL,
    `pro_before` DECIMAL(36,18) NOT NULL,
    `pro_delta` DECIMAL(36,18) NOT NULL,
    `pro_after` DECIMAL(36,18) NOT NULL,
    `was_pro` TINYINT NOT NULL,
    `unlocked_pro` TINYINT NOT NULL,
    `voucher_id` BIGINT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_completion_reward` (`completion_id`, `user_id`, `reason`),
    KEY `ix_reward_user` (`user_id`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
