CREATE TABLE `deposit_quote_confirmation` (
    `quote_idx` CHAR(32) NOT NULL,
    `user_id` BIGINT NOT NULL,
    `snapshot_json` TEXT NOT NULL,
    `quote_expires_at` DATETIME(3) NOT NULL,
    `confirmed_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`quote_idx`),
    KEY `ix_deposit_quote_user` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;

ALTER TABLE `deposit_quote_confirmation`
 ADD CONSTRAINT `fk_deposit_quote_user` FOREIGN KEY (`user_id`) REFERENCES `user` (`id`),
 ADD CONSTRAINT `ck_deposit_quote_window` CHECK (`confirmed_at` < `quote_expires_at`);
