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
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
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
    `verified_at` DATETIME(6) NOT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_task_identity` (`task_id`, `telegram_identity_id`),
    UNIQUE KEY `uk_task_request` (`user_id`, `request_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
