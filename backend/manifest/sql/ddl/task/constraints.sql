ALTER TABLE `task_definition`
    ADD CONSTRAINT `ck_task_kind` CHECK (`kind` = _ascii'telegram_channel_follow'),
    ADD CONSTRAINT `ck_task_status` CHECK (`status` IN (_ascii'draft',_ascii'active',_ascii'paused')),
    ADD CONSTRAINT `ck_task_mode` CHECK (`verification_mode` = _ascii'mock'),
    ADD CONSTRAINT `ck_task_values` CHECK (`base_points` > 0 AND `channel_id` < 0 AND `revision` > 0 AND `ends_at` > `starts_at`);
ALTER TABLE `task_completion`
    ADD CONSTRAINT `fk_task_completion_task` FOREIGN KEY (`task_id`) REFERENCES `task_definition` (`id`),
    ADD CONSTRAINT `fk_task_completion_identity` FOREIGN KEY (`telegram_identity_id`) REFERENCES `telegram_identity` (`id`),
    ADD CONSTRAINT `fk_task_completion_user` FOREIGN KEY (`user_id`) REFERENCES `user` (`id`),
    ADD CONSTRAINT `ck_task_completion_values` CHECK (`task_revision` > 0 AND `base_points` > 0);

ALTER TABLE `task_completion`
    ADD CONSTRAINT `fk_completion_invitation` FOREIGN KEY (`invitation_id`) REFERENCES `task_invitation` (`id`),
    ADD CONSTRAINT `ck_completion_mode` CHECK ((`mode` = 'self' AND `invitation_id` IS NULL) OR (`mode` = 'help' AND `invitation_id` IS NOT NULL)),
    ADD CONSTRAINT `ck_completion_stage` CHECK (`participant_stage` IN ('first','returning')),
    ADD CONSTRAINT `ck_completion_version` CHECK (`rule_version` = 0 OR (`rule_version` = 1 AND `identity_revision` >= 0 AND `binding_version` > 0 AND `member_status` IN ('creator','administrator','member')));
ALTER TABLE `user_reward_state`
    ADD CONSTRAINT `fk_reward_state_user` FOREIGN KEY (`user_id`) REFERENCES `user` (`id`),
    ADD CONSTRAINT `fk_reward_state_first` FOREIGN KEY (`first_completion_id`) REFERENCES `task_completion` (`id`),
    ADD CONSTRAINT `ck_reward_progress` CHECK (`pro_progress` >= 0 AND `pro_progress` = FLOOR(`pro_progress`)),
    ADD CONSTRAINT `ck_reward_pro` CHECK ((`pro_progress` >= 2000) = (`pro_unlocked_at` IS NOT NULL));
ALTER TABLE `task_invitation`
    ADD CONSTRAINT `fk_invitation_task` FOREIGN KEY (`task_id`) REFERENCES `task_definition` (`id`),
    ADD CONSTRAINT `fk_invitation_user` FOREIGN KEY (`inviter_user_id`) REFERENCES `user` (`id`);
ALTER TABLE `user_referral`
    ADD CONSTRAINT `fk_referral_user` FOREIGN KEY (`user_id`) REFERENCES `user` (`id`),
    ADD CONSTRAINT `fk_referral_parent` FOREIGN KEY (`parent_user_id`) REFERENCES `user` (`id`),
    ADD CONSTRAINT `fk_referral_completion` FOREIGN KEY (`completion_id`) REFERENCES `task_completion` (`id`),
    ADD CONSTRAINT `ck_referral_self` CHECK (`user_id` <> `parent_user_id`);
ALTER TABLE `reward_grant`
    ADD CONSTRAINT `fk_grant_completion` FOREIGN KEY (`completion_id`) REFERENCES `task_completion` (`id`),
    ADD CONSTRAINT `fk_grant_user` FOREIGN KEY (`user_id`) REFERENCES `user` (`id`),
    ADD CONSTRAINT `fk_grant_voucher` FOREIGN KEY (`voucher_id`) REFERENCES `ledger_voucher` (`id`),
    ADD CONSTRAINT `ck_grant_reason` CHECK (`reason` IN ('first_task','own_task','assistance','commission')),
    ADD CONSTRAINT `ck_grant_amounts` CHECK (`reward_usd` >= 0 AND `reward_usd` = TRUNCATE(`reward_usd`,2) AND `points` >= 0 AND `points` = FLOOR(`points`) AND (`reward_usd` > 0 OR `points` > 0)),
    ADD CONSTRAINT `ck_grant_progress` CHECK (`pro_before` >= 0 AND `pro_delta` >= 0 AND `pro_after` = `pro_before` + `pro_delta` AND `pro_before` = FLOOR(`pro_before`) AND `pro_delta` = FLOOR(`pro_delta`)),
    ADD CONSTRAINT `ck_grant_flags` CHECK (`was_pro` IN (0,1) AND `unlocked_pro` IN (0,1));
