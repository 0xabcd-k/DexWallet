-- Legacy held balances have no source records in 3.2. Refuse to invent history.
CREATE TABLE `hold_migration_guard` (`unattributed` BIGINT NOT NULL CHECK (`unattributed` = 0));
INSERT INTO `hold_migration_guard` SELECT COUNT(*) FROM `user_account` WHERE `held_balance` <> 0;
DROP TABLE `hold_migration_guard`;
ALTER TABLE `user_account` ADD CONSTRAINT `ck_user_account_release_capacity` CHECK (`balance` + `held_balance` < 1000000000000000000);
ALTER TABLE `ledger_entry` ADD COLUMN `balance_part` VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '';
UPDATE `ledger_entry` SET `balance_part` = 'available' WHERE `account_id` IS NOT NULL;
ALTER TABLE `ledger_entry` DROP CHECK `ck_ledger_target`, DROP CHECK `ck_ledger_unit`,
    ADD CONSTRAINT `ck_ledger_target` CHECK (
        (`account_id` IS NOT NULL AND `system_code` = '' AND `balance_after` IS NOT NULL AND `balance_after` >= 0 AND `balance_part` IN ('available', 'held')) OR
        (`account_id` IS NULL AND `system_code` <> '' AND `balance_after` IS NULL AND `balance_part` = '')
    ),
    ADD CONSTRAINT `ck_ledger_unit` CHECK (`unit` IN ('USD', 'POINT') AND (`balance_part` <> 'held' OR `unit` = 'USD'));

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

ALTER TABLE `account_hold`
    ADD CONSTRAINT `fk_hold_account` FOREIGN KEY (`account_id`) REFERENCES `user_account` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `fk_hold_voucher` FOREIGN KEY (`hold_voucher_id`) REFERENCES `ledger_voucher` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `fk_hold_resolved_voucher` FOREIGN KEY (`resolved_voucher_id`) REFERENCES `ledger_voucher` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `ck_hold_source` CHECK (`business_id` > 0 AND `business_type` IN ('withdrawal', 'payment')),
    ADD CONSTRAINT `ck_hold_amount` CHECK (`amount` > 0 AND `amount` = TRUNCATE(`amount`, 2)),
    ADD CONSTRAINT `ck_hold_resolution` CHECK (
        (`status` = 'held' AND `resolved_voucher_id` IS NULL AND `resolved_at` IS NULL AND `reason` = '') OR
        (`status` = 'consumed' AND `resolved_voucher_id` IS NOT NULL AND `resolved_at` IS NOT NULL AND `reason` = 'completed') OR
        (`status` = 'released' AND `resolved_voucher_id` IS NOT NULL AND `resolved_at` IS NOT NULL AND `reason` IN ('cancelled', 'rejected', 'not_executed'))
    ),
    ADD CONSTRAINT `ck_hold_vouchers` CHECK (`resolved_voucher_id` IS NULL OR `resolved_voucher_id` <> `hold_voucher_id`);
