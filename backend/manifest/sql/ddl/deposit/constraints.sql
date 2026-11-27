ALTER TABLE `deposit_quote_confirmation`
 ADD CONSTRAINT `fk_deposit_quote_user` FOREIGN KEY (`user_id`) REFERENCES `user` (`id`),
 ADD CONSTRAINT `ck_deposit_quote_window` CHECK (`confirmed_at` < `quote_expires_at`);

ALTER TABLE `sol_deposit_order`
 ADD CONSTRAINT `fk_sol_deposit_quote` FOREIGN KEY (`quote_idx`) REFERENCES `deposit_quote_confirmation` (`quote_idx`),
 ADD CONSTRAINT `ck_sol_deposit_height` CHECK (`last_valid_height` > 0),
 ADD CONSTRAINT `ck_sol_deposit_signature` CHECK ((`user_signature` = '' AND `accepted_at` IS NULL) OR (`user_signature` <> '' AND `accepted_at` IS NOT NULL));
