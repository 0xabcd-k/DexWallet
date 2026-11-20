ALTER TABLE `solana_payout`
 ADD COLUMN `recovery_state` VARCHAR(24) NOT NULL DEFAULT 'ready' AFTER `attempts`,
 ADD COLUMN `recovery_attempts` BIGINT NOT NULL DEFAULT 0 AFTER `recovery_state`,
 ADD COLUMN `recovery_generation` BIGINT NOT NULL DEFAULT 0 AFTER `recovery_attempts`,
 ADD COLUMN `recovery_next_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) AFTER `recovery_generation`,
 ADD COLUMN `recovery_lease_until` DATETIME(6) NULL AFTER `recovery_next_at`,
 ADD COLUMN `recovery_checked_at` DATETIME(6) NULL AFTER `recovery_lease_until`,
 ADD COLUMN `recovery_error` VARCHAR(2048) CHARACTER SET utf8mb4 NOT NULL DEFAULT '' AFTER `recovery_checked_at`,
 ADD COLUMN `recovery_retry_note` VARCHAR(512) CHARACTER SET utf8mb4 NOT NULL DEFAULT '' AFTER `recovery_error`,
 ADD KEY `ix_sol_recovery_due` (`sender`, `recovery_state`, `recovery_next_at`, `id`),
 ADD KEY `ix_sol_recovery_lease` (`sender`, `recovery_state`, `recovery_lease_until`, `id`);
UPDATE `solana_payout` SET `recovery_state`='confirmed', `recovery_checked_at`=`updated_at` WHERE `state`='confirmed';
ALTER TABLE `solana_payout`
 ADD CONSTRAINT `ck_sol_recovery_state` CHECK (`recovery_state` IN ('ready','running','review','confirmed') AND ((`state`='confirmed')=(`recovery_state`='confirmed'))),
 ADD CONSTRAINT `ck_sol_recovery_counts` CHECK (`recovery_attempts` >= 0 AND `recovery_generation` >= `recovery_attempts`),
 ADD CONSTRAINT `ck_sol_recovery_lease` CHECK ((`recovery_state`='running')=(`recovery_lease_until` IS NOT NULL));
