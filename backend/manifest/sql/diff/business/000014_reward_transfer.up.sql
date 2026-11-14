CREATE TABLE `reward_transfer` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `user_id` BIGINT NOT NULL,
    `request_key` VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `amount` DECIMAL(36,18) NOT NULL,
    `status` VARCHAR(24) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `risk_decision` VARCHAR(24) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `risk_version` VARCHAR(64) NOT NULL,
    `risk_reason` VARCHAR(512) NOT NULL,
    `submission_voucher_id` BIGINT NULL,
    `resolution_voucher_id` BIGINT NULL,
    `reviewer_id` BIGINT NULL,
    `review_reason` VARCHAR(512) NOT NULL DEFAULT '',
    `resolved_at` DATETIME(6) NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_reward_transfer_request` (`user_id`, `request_key`),
    KEY `ix_reward_transfer_user` (`user_id`, `id`),
    KEY `ix_reward_transfer_status` (`status`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

ALTER TABLE `reward_transfer`
    ADD CONSTRAINT `fk_reward_transfer_user` FOREIGN KEY (`user_id`) REFERENCES `user` (`id`),
    ADD CONSTRAINT `fk_reward_transfer_submission` FOREIGN KEY (`submission_voucher_id`) REFERENCES `ledger_voucher` (`id`),
    ADD CONSTRAINT `fk_reward_transfer_resolution` FOREIGN KEY (`resolution_voucher_id`) REFERENCES `ledger_voucher` (`id`),
    ADD CONSTRAINT `fk_reward_transfer_reviewer` FOREIGN KEY (`reviewer_id`) REFERENCES `admin_user` (`id`),
    ADD CONSTRAINT `ck_reward_transfer_amount` CHECK (`amount` >= 2 AND `amount` = TRUNCATE(`amount`,2)),
    ADD CONSTRAINT `ck_reward_transfer_status` CHECK (`status` IN (_ascii'pending_review',_ascii'completed',_ascii'rejected')),
    ADD CONSTRAINT `ck_reward_transfer_risk` CHECK (`risk_decision` IN (_ascii'allow',_ascii'review'));
