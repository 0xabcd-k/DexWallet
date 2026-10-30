ALTER TABLE `user_account`
    ADD CONSTRAINT `fk_user_account_user` FOREIGN KEY (`user_id`) REFERENCES `user` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
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
        (`account_id` IS NOT NULL AND `system_code` = '' AND `balance_after` IS NOT NULL AND `balance_after` >= 0) OR
        (`account_id` IS NULL AND `system_code` <> '' AND `balance_after` IS NULL)
    ),
    ADD CONSTRAINT `ck_ledger_amount` CHECK (`amount` > 0 AND `line_no` > 0 AND `side` IN ('debit', 'credit')),
    ADD CONSTRAINT `ck_ledger_unit` CHECK (`unit` IN ('USD', 'POINT')),
    ADD CONSTRAINT `ck_ledger_precision` CHECK (
        (`unit` = 'USD' AND `amount` = TRUNCATE(`amount`, 2) AND (`balance_after` IS NULL OR `balance_after` = TRUNCATE(`balance_after`, 2))) OR
        (`unit` = 'POINT' AND `amount` = FLOOR(`amount`) AND (`balance_after` IS NULL OR `balance_after` = FLOOR(`balance_after`)))
    );
