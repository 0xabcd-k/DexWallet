CREATE TABLE `user_account` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `user_id` BIGINT NOT NULL,
    `kind` VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `balance` DECIMAL(36,18) NOT NULL DEFAULT 0,
    `held_balance` DECIMAL(36,18) NOT NULL DEFAULT 0,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_user_account_kind` (`user_id`, `kind`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `ledger_voucher` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `business_type` VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `business_id` BIGINT NOT NULL,
    `action` VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `original_voucher_id` BIGINT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_ledger_source` (`business_type`, `business_id`, `action`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `ledger_entry` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `voucher_id` BIGINT NOT NULL,
    `line_no` INT NOT NULL,
    `account_id` BIGINT NULL,
    `system_code` VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    `unit` VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `side` VARCHAR(6) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `amount` DECIMAL(36,18) NOT NULL,
    `balance_after` DECIMAL(36,18) NULL,
    `balance_part` VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_ledger_line` (`voucher_id`, `line_no`),
    KEY `ix_ledger_account_entry` (`account_id`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `account_hold` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `account_id` BIGINT NOT NULL,
    `business_type` VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `business_id` BIGINT NOT NULL,
    `amount` DECIMAL(36,18) NOT NULL,
    `status` VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `hold_voucher_id` BIGINT NOT NULL,
    `resolved_voucher_id` BIGINT NULL,
    `reason` VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `resolved_at` DATETIME(6) NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_hold_source` (`business_type`, `business_id`),
    UNIQUE KEY `uk_hold_voucher` (`hold_voucher_id`),
    UNIQUE KEY `uk_hold_resolved_voucher` (`resolved_voucher_id`),
    KEY `ix_hold_account_status` (`account_id`, `status`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
