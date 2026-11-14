-- Existing rule_version=0 completions remain legacy facts; never infer or backfill rewards.
ALTER TABLE `task_completion`
    ADD COLUMN `mode` VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'self' AFTER `base_points`,
    ADD COLUMN `participant_stage` VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'first' AFTER `mode`,
    ADD COLUMN `invitation_id` BIGINT NULL AFTER `participant_stage`,
    ADD COLUMN `identity_revision` BIGINT NOT NULL DEFAULT 0 AFTER `invitation_id`,
    ADD COLUMN `binding_version` BIGINT NOT NULL DEFAULT 0 AFTER `identity_revision`,
    ADD COLUMN `member_status` VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '' AFTER `binding_version`,
    ADD COLUMN `task_title` VARCHAR(128) NOT NULL DEFAULT '' AFTER `member_status`,
    ADD COLUMN `rule_version` BIGINT NOT NULL DEFAULT 0 AFTER `task_title`,
    ADD KEY `ix_completion_user` (`user_id`, `id`),
    ADD KEY `ix_completion_user_task` (`user_id`, `task_id`, `id`);

CREATE TABLE `user_reward_state` (
    `user_id` BIGINT NOT NULL,
    `first_completion_id` BIGINT NULL,
    `pro_progress` DECIMAL(36,18) NOT NULL DEFAULT 0,
    `pro_unlocked_at` DATETIME(6) NULL,
    PRIMARY KEY (`user_id`),
    UNIQUE KEY `uk_reward_first_completion` (`first_completion_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `task_invitation` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `task_id` BIGINT NOT NULL,
    `inviter_user_id` BIGINT NOT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_task_inviter` (`task_id`, `inviter_user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `user_referral` (
    `user_id` BIGINT NOT NULL,
    `parent_user_id` BIGINT NOT NULL,
    `completion_id` BIGINT NOT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`user_id`),
    UNIQUE KEY `uk_referral_completion` (`completion_id`),
    KEY `ix_referral_parent` (`parent_user_id`, `user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `reward_grant` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `completion_id` BIGINT NOT NULL,
    `user_id` BIGINT NOT NULL,
    `reason` VARCHAR(24) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `reward_usd` DECIMAL(36,18) NOT NULL,
    `points` DECIMAL(36,18) NOT NULL,
    `pro_before` DECIMAL(36,18) NOT NULL,
    `pro_delta` DECIMAL(36,18) NOT NULL,
    `pro_after` DECIMAL(36,18) NOT NULL,
    `was_pro` TINYINT NOT NULL,
    `unlocked_pro` TINYINT NOT NULL,
    `voucher_id` BIGINT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_completion_reward` (`completion_id`, `user_id`, `reason`),
    KEY `ix_reward_user` (`user_id`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

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
