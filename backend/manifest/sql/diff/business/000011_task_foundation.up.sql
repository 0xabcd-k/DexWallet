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

ALTER TABLE `task_definition`
    ADD CONSTRAINT `ck_task_kind` CHECK (`kind` = 'telegram_channel_follow'),
    ADD CONSTRAINT `ck_task_status` CHECK (`status` IN ('draft','active','paused')),
    ADD CONSTRAINT `ck_task_mode` CHECK (`verification_mode` = 'mock'),
    ADD CONSTRAINT `ck_task_values` CHECK (`base_points` > 0 AND `channel_id` < 0 AND `revision` > 0 AND `ends_at` > `starts_at`);
ALTER TABLE `task_completion`
    ADD CONSTRAINT `fk_task_completion_task` FOREIGN KEY (`task_id`) REFERENCES `task_definition` (`id`),
    ADD CONSTRAINT `fk_task_completion_identity` FOREIGN KEY (`telegram_identity_id`) REFERENCES `telegram_identity` (`id`),
    ADD CONSTRAINT `fk_task_completion_user` FOREIGN KEY (`user_id`) REFERENCES `user` (`id`),
    ADD CONSTRAINT `ck_task_completion_values` CHECK (`task_revision` > 0 AND `base_points` > 0);
