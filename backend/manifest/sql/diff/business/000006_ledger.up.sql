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
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_ledger_line` (`voucher_id`, `line_no`),
    KEY `ix_ledger_account_entry` (`account_id`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

ALTER TABLE `ledger_voucher`
    ADD CONSTRAINT `fk_ledger_original` FOREIGN KEY (`original_voucher_id`) REFERENCES `ledger_voucher` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `ck_ledger_source` CHECK (`business_id` > 0 AND `business_type` <> '' AND `action` <> '');

ALTER TABLE `ledger_entry`
    ADD CONSTRAINT `fk_ledger_voucher` FOREIGN KEY (`voucher_id`) REFERENCES `ledger_voucher` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `fk_ledger_account` FOREIGN KEY (`account_id`) REFERENCES `user_account` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `ck_ledger_target` CHECK (
        (`account_id` IS NOT NULL AND `system_code` = '' AND `balance_after` IS NOT NULL AND `balance_after` >= 0) OR
        (`account_id` IS NULL AND `system_code` <> '' AND `balance_after` IS NULL)
    ),
    ADD CONSTRAINT `ck_ledger_amount` CHECK (`amount` > 0 AND `line_no` > 0 AND `side` IN ('debit', 'credit')),
    ADD CONSTRAINT `ck_ledger_unit` CHECK (`unit` IN ('USD', 'POINT')),
    ADD CONSTRAINT `ck_ledger_precision` CHECK (
        (`unit` = 'USD' AND `amount` = TRUNCATE(`amount`, 2) AND (`balance_after` IS NULL OR `balance_after` = TRUNCATE(`balance_after`, 2))) OR
        (`unit` = 'POINT' AND `amount` = FLOOR(`amount`) AND (`balance_after` IS NULL OR `balance_after` = FLOOR(`balance_after`)))
    );
