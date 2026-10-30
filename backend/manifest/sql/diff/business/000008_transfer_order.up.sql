CREATE TABLE `transfer_order` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `sender_id` BIGINT NOT NULL,
    `recipient_id` BIGINT NOT NULL,
    `request_key` VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `amount` DECIMAL(36,18) NOT NULL,
    `voucher_id` BIGINT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_transfer_request` (`sender_id`, `request_key`),
    UNIQUE KEY `uk_transfer_voucher` (`voucher_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

ALTER TABLE `transfer_order`
    ADD CONSTRAINT `fk_transfer_sender` FOREIGN KEY (`sender_id`) REFERENCES `user` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `fk_transfer_recipient` FOREIGN KEY (`recipient_id`) REFERENCES `user` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `fk_transfer_voucher` FOREIGN KEY (`voucher_id`) REFERENCES `ledger_voucher` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `ck_transfer_parties` CHECK (`sender_id` > 0 AND `recipient_id` > 0 AND `sender_id` <> `recipient_id`),
    ADD CONSTRAINT `ck_transfer_amount` CHECK (`amount` > 0 AND `amount` = TRUNCATE(`amount`, 2)),
    ADD CONSTRAINT `ck_transfer_request` CHECK (CHAR_LENGTH(`request_key`) BETWEEN 16 AND 64);
