ALTER TABLE `transfer_order`
    ADD CONSTRAINT `fk_transfer_sender` FOREIGN KEY (`sender_id`) REFERENCES `user` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `fk_transfer_recipient` FOREIGN KEY (`recipient_id`) REFERENCES `user` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `fk_transfer_voucher` FOREIGN KEY (`voucher_id`) REFERENCES `ledger_voucher` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `ck_transfer_parties` CHECK (`sender_id` > 0 AND `recipient_id` > 0 AND `sender_id` <> `recipient_id`),
    ADD CONSTRAINT `ck_transfer_amount` CHECK (`amount` > 0 AND `amount` = TRUNCATE(`amount`, 2)),
    ADD CONSTRAINT `ck_transfer_request` CHECK (CHAR_LENGTH(`request_key`) BETWEEN 16 AND 64);
