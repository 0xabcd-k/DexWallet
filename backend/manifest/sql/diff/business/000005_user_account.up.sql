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

ALTER TABLE `user_account`
    ADD CONSTRAINT `fk_user_account_user` FOREIGN KEY (`user_id`) REFERENCES `user` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `ck_user_account_nonnegative` CHECK (`balance` >= 0 AND `held_balance` >= 0),
    ADD CONSTRAINT `ck_user_account_points` CHECK (`kind` <> 'task_points' OR `balance` = FLOOR(`balance`)),
    ADD CONSTRAINT `ck_user_account_usd_precision` CHECK (`kind` NOT IN ('usd_balance', 'reward_usd') OR (`balance` = TRUNCATE(`balance`, 2) AND `held_balance` = TRUNCATE(`held_balance`, 2))),
    ADD CONSTRAINT `ck_user_account_held` CHECK (`kind` = 'usd_balance' OR `held_balance` = 0);
