ALTER TABLE `user_account`
    ADD CONSTRAINT `fk_user_account_user` FOREIGN KEY (`user_id`) REFERENCES `user` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `ck_user_account_nonnegative` CHECK (`balance` >= 0 AND `held_balance` >= 0),
    ADD CONSTRAINT `ck_user_account_points` CHECK (`kind` <> 'task_points' OR `balance` = FLOOR(`balance`)),
    ADD CONSTRAINT `ck_user_account_usd_precision` CHECK (`kind` NOT IN ('usd_balance', 'reward_usd') OR (`balance` = TRUNCATE(`balance`, 2) AND `held_balance` = TRUNCATE(`held_balance`, 2))),
    ADD CONSTRAINT `ck_user_account_held` CHECK (`kind` = 'usd_balance' OR `held_balance` = 0);
