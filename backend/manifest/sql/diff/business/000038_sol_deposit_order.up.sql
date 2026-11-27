CREATE TABLE `sol_deposit_order` (
    `quote_idx` CHAR(32) NOT NULL,
    `order_idx` CHAR(32) NOT NULL,
    `message_base64` VARCHAR(512) NOT NULL,
    `message_sha256` CHAR(64) NOT NULL,
    `last_valid_height` BIGINT NOT NULL,
    `review_after` DATETIME(3) NOT NULL,
    `user_signature` VARCHAR(88) NOT NULL DEFAULT '',
    `accepted_at` DATETIME(3) NULL,
    PRIMARY KEY (`quote_idx`),
    UNIQUE KEY `uk_sol_deposit_order` (`order_idx`),
    UNIQUE KEY `uk_sol_deposit_message` (`message_sha256`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;

ALTER TABLE `sol_deposit_order`
 ADD CONSTRAINT `fk_sol_deposit_quote` FOREIGN KEY (`quote_idx`) REFERENCES `deposit_quote_confirmation` (`quote_idx`),
 ADD CONSTRAINT `ck_sol_deposit_height` CHECK (`last_valid_height` > 0),
 ADD CONSTRAINT `ck_sol_deposit_signature` CHECK ((`user_signature` = '' AND `accepted_at` IS NULL) OR (`user_signature` <> '' AND `accepted_at` IS NOT NULL));
