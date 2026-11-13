ALTER TABLE `transfer_order`
    ADD CONSTRAINT `fk_transfer_sender` FOREIGN KEY (`sender_id`) REFERENCES `user` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `fk_transfer_recipient` FOREIGN KEY (`recipient_id`) REFERENCES `user` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `fk_transfer_voucher` FOREIGN KEY (`voucher_id`) REFERENCES `ledger_voucher` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `ck_transfer_parties` CHECK (`sender_id` > 0 AND `recipient_id` > 0 AND `sender_id` <> `recipient_id`),
    ADD CONSTRAINT `ck_transfer_amount` CHECK (`amount` > 0 AND `amount` = TRUNCATE(`amount`, 2)),
    ADD CONSTRAINT `ck_transfer_request` CHECK (CHAR_LENGTH(`request_key`) BETWEEN 16 AND 64);
ALTER TABLE `transfer_limit_plan`
    ADD CONSTRAINT `ck_limit_plan_amounts` CHECK (min_amount > 0 AND min_amount <= max_amount AND max_amount <= daily_outgoing_limit AND min_amount = TRUNCATE(min_amount,2) AND max_amount = TRUNCATE(max_amount,2) AND daily_outgoing_limit = TRUNCATE(daily_outgoing_limit,2));
ALTER TABLE `user` ADD CONSTRAINT `fk_user_transfer_plan` FOREIGN KEY (`transfer_limit_plan_id`) REFERENCES `transfer_limit_plan` (`id`);
ALTER TABLE `transfer_order` ADD CONSTRAINT `fk_transfer_plan` FOREIGN KEY (`limit_plan_id`) REFERENCES `transfer_limit_plan` (`id`);
