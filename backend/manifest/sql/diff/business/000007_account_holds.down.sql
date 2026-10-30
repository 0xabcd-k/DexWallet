-- Never erase reservation history or turn held entries into available entries.
CREATE TABLE `hold_rollback_guard` (`history_count` BIGINT NOT NULL CHECK (`history_count` = 0));
INSERT INTO `hold_rollback_guard` SELECT (SELECT COUNT(*) FROM `account_hold`) + (SELECT COUNT(*) FROM `ledger_entry` WHERE `balance_part` = 'held') + (SELECT COUNT(*) FROM `user_account` WHERE `held_balance` <> 0);
DROP TABLE `hold_rollback_guard`;
DROP TABLE `account_hold`;
ALTER TABLE `user_account` DROP CHECK `ck_user_account_release_capacity`;
ALTER TABLE `ledger_entry` DROP CHECK `ck_ledger_target`, DROP CHECK `ck_ledger_unit`,
    ADD CONSTRAINT `ck_ledger_target` CHECK (
        (`account_id` IS NOT NULL AND `system_code` = '' AND `balance_after` IS NOT NULL AND `balance_after` >= 0) OR
        (`account_id` IS NULL AND `system_code` <> '' AND `balance_after` IS NULL)
    ),
    ADD CONSTRAINT `ck_ledger_unit` CHECK (`unit` IN ('USD', 'POINT'));
ALTER TABLE `ledger_entry` DROP COLUMN `balance_part`;
