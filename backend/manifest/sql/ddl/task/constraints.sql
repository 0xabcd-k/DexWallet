ALTER TABLE `task_definition`
    ADD CONSTRAINT `ck_task_kind` CHECK (`kind` = 'telegram_channel_follow'),
    ADD CONSTRAINT `ck_task_status` CHECK (`status` IN ('draft','active','paused')),
    ADD CONSTRAINT `ck_task_mode` CHECK (`verification_mode` = 'mock'),
    ADD CONSTRAINT `ck_task_values` CHECK (`base_points` > 0 AND `channel_id` < 0 AND `revision` > 0 AND `ends_at` > `starts_at`);
ALTER TABLE `task_completion`
    ADD CONSTRAINT `fk_task_completion_task` FOREIGN KEY (`task_id`) REFERENCES `task_definition` (`id`),
    ADD CONSTRAINT `fk_task_completion_identity` FOREIGN KEY (`telegram_identity_id`) REFERENCES `telegram_identity` (`id`),
    ADD CONSTRAINT `fk_task_completion_user` FOREIGN KEY (`user_id`) REFERENCES `user` (`id`),
    ADD CONSTRAINT `ck_task_completion_values` CHECK (`task_revision` > 0 AND `base_points` > 0);
