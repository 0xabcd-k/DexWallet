ALTER TABLE `email_identity` ADD CONSTRAINT `fk_email_user`
    FOREIGN KEY (`user_id`) REFERENCES `user` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT;
ALTER TABLE `telegram_identity` ADD CONSTRAINT `fk_telegram_user`
    FOREIGN KEY (`user_id`) REFERENCES `user` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT;
