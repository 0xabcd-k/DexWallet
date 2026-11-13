CREATE TABLE `transfer_limit_plan` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `name` VARCHAR(128) NOT NULL,
    `min_amount` DECIMAL(36,18) NOT NULL,
    `max_amount` DECIMAL(36,18) NOT NULL,
    `daily_outgoing_limit` DECIMAL(36,18) NOT NULL,
    `creator_id` BIGINT NOT NULL,
    `request_key` VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_limit_plan_request` (`creator_id`, `request_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `transfer_order` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `sender_id` BIGINT NOT NULL,
    `recipient_id` BIGINT NOT NULL,
    `request_key` VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `amount` DECIMAL(36,18) NOT NULL,
    `voucher_id` BIGINT NULL,
    `limit_plan_id` BIGINT NULL,
    `sender_nickname` VARCHAR(128) NOT NULL DEFAULT '',
    `recipient_nickname` VARCHAR(128) NOT NULL DEFAULT '',
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_transfer_request` (`sender_id`, `request_key`),
    UNIQUE KEY `uk_transfer_voucher` (`voucher_id`),
    KEY `fk_transfer_recipient` (`recipient_id`),
    KEY `ix_transfer_sender_day` (`sender_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
