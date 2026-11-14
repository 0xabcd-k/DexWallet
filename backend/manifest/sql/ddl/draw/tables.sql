CREATE TABLE `draw_rule_version` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `name` VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `rules_json` TEXT NOT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_draw_rule_name` (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `draw_control` (
    `id` BIGINT NOT NULL,
    `version_id` BIGINT NULL,
    PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `point_draw` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `user_id` BIGINT NOT NULL,
    `request_key` VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `version_id` BIGINT NOT NULL,
    `cost_points` BIGINT NOT NULL,
    `prize_code` VARCHAR(24) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `reward_usd` DECIMAL(36,18) NOT NULL,
    `symbols` VARCHAR(5) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `ticket` BIGINT NOT NULL,
    `voucher_id` BIGINT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_draw_request` (`user_id`, `request_key`),
    KEY `ix_draw_user` (`user_id`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
