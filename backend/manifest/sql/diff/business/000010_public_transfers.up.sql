CREATE TABLE `transfer_limit_plan` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `name` VARCHAR(128) NOT NULL,
    `min_amount` DECIMAL(36,18) NOT NULL,
    `max_amount` DECIMAL(36,18) NOT NULL,
    `daily_outgoing_limit` DECIMAL(36,18) NOT NULL,
    `creator_id` BIGINT NOT NULL,
    `request_key` VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_limit_plan_request` (`creator_id`, `request_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
INSERT INTO transfer_limit_plan (id,name,min_amount,max_amount,daily_outgoing_limit,creator_id,request_key) VALUES (1,'Default USD',0.01,100,500,0,'initial_default_plan');
ALTER TABLE `user`
    ADD COLUMN `transfer_limit_plan_id` BIGINT NOT NULL DEFAULT 1 AFTER auth_version,
    ADD COLUMN `payment_restricted_until` DATETIME(6) NULL AFTER transfer_limit_plan_id,
    ADD COLUMN `identity_revision` BIGINT NOT NULL DEFAULT 1 AFTER payment_restricted_until,
    ADD COLUMN `last_email_identity_id` BIGINT NOT NULL DEFAULT -1 AFTER identity_revision,
    ADD COLUMN `last_telegram_identity_id` BIGINT NOT NULL DEFAULT -1 AFTER last_email_identity_id;
-- Existing unbound history cannot be reconstructed from identity.user_id.
-- -1 is explicitly unknown; 0 is verified never-bound (new accounts only).
UPDATE `user` u JOIN email_identity e ON e.user_id=u.id SET u.last_email_identity_id=e.id;
UPDATE `user` u JOIN telegram_identity t ON t.user_id=u.id SET u.last_telegram_identity_id=t.id;
ALTER TABLE `user` ALTER COLUMN last_email_identity_id SET DEFAULT 0, ALTER COLUMN last_telegram_identity_id SET DEFAULT 0;
ALTER TABLE `transfer_order`
    ADD COLUMN `limit_plan_id` BIGINT NULL AFTER voucher_id,
    ADD COLUMN `sender_nickname` VARCHAR(128) NOT NULL DEFAULT '' AFTER limit_plan_id,
    ADD COLUMN `recipient_nickname` VARCHAR(128) NOT NULL DEFAULT '' AFTER sender_nickname,
    ADD KEY `ix_transfer_sender_day` (`sender_id`,`created_at`);
ALTER TABLE `transfer_limit_plan`
    ADD CONSTRAINT `ck_limit_plan_amounts` CHECK (min_amount > 0 AND min_amount <= max_amount AND max_amount <= daily_outgoing_limit AND min_amount = TRUNCATE(min_amount,2) AND max_amount = TRUNCATE(max_amount,2) AND daily_outgoing_limit = TRUNCATE(daily_outgoing_limit,2));
ALTER TABLE `user` ADD CONSTRAINT `fk_user_transfer_plan` FOREIGN KEY (`transfer_limit_plan_id`) REFERENCES `transfer_limit_plan` (`id`);
ALTER TABLE `transfer_order` ADD CONSTRAINT `fk_transfer_plan` FOREIGN KEY (`limit_plan_id`) REFERENCES `transfer_limit_plan` (`id`);
