ALTER TABLE `deposit_quote_confirmation`
 ADD CONSTRAINT `fk_deposit_quote_user` FOREIGN KEY (`user_id`) REFERENCES `user` (`id`),
 ADD CONSTRAINT `ck_deposit_quote_window` CHECK (`confirmed_at` < `quote_expires_at`);
