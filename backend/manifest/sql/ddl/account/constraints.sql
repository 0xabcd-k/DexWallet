ALTER TABLE `user_account`
    ADD CONSTRAINT `fk_user_account_user` FOREIGN KEY (`user_id`) REFERENCES `user` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `ck_user_account_release_capacity` CHECK (`balance` + `held_balance` < 1000000000000000000),
    ADD CONSTRAINT `ck_user_account_nonnegative` CHECK (`balance` >= 0 AND `held_balance` >= 0),
    ADD CONSTRAINT `ck_user_account_points` CHECK (`kind` <> 'task_points' OR `balance` = FLOOR(`balance`)),
    ADD CONSTRAINT `ck_user_account_usd_precision` CHECK (`kind` NOT IN ('usd_balance', 'reward_usd') OR (`balance` = TRUNCATE(`balance`, 2) AND `held_balance` = TRUNCATE(`held_balance`, 2))),
    ADD CONSTRAINT `ck_user_account_held` CHECK (`kind` = 'usd_balance' OR `held_balance` = 0);

ALTER TABLE `ledger_voucher`
    ADD CONSTRAINT `fk_ledger_original` FOREIGN KEY (`original_voucher_id`) REFERENCES `ledger_voucher` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `ck_ledger_source` CHECK (`business_id` > 0 AND `business_type` <> '' AND `action` <> '');

ALTER TABLE `ledger_entry`
    ADD CONSTRAINT `fk_ledger_voucher` FOREIGN KEY (`voucher_id`) REFERENCES `ledger_voucher` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `fk_ledger_account` FOREIGN KEY (`account_id`) REFERENCES `user_account` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `ck_ledger_target` CHECK (
        (`account_id` IS NOT NULL AND `system_code` = '' AND `balance_after` IS NOT NULL AND `balance_after` >= 0 AND `balance_part` IN ('available', 'held')) OR
        (`account_id` IS NULL AND `system_code` <> '' AND `balance_after` IS NULL AND `balance_part` = '')
    ),
    ADD CONSTRAINT `ck_ledger_amount` CHECK (`amount` > 0 AND `line_no` > 0 AND `side` IN ('debit', 'credit')),
    ADD CONSTRAINT `ck_ledger_unit` CHECK (`unit` IN ('USD', 'POINT') AND (`balance_part` <> 'held' OR `unit` = 'USD')),
    ADD CONSTRAINT `ck_ledger_precision` CHECK (
        (`unit` = 'USD' AND `amount` = TRUNCATE(`amount`, 2) AND (`balance_after` IS NULL OR `balance_after` = TRUNCATE(`balance_after`, 2))) OR
        (`unit` = 'POINT' AND `amount` = FLOOR(`amount`) AND (`balance_after` IS NULL OR `balance_after` = FLOOR(`balance_after`)))
    );

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
