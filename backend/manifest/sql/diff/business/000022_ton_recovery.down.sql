-- Do not discard pending signed payments or observed recovery history.
CREATE TABLE `ton_recovery_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used` = 0));
INSERT INTO `ton_recovery_rollback_guard` SELECT COUNT(*) FROM `chain_send_intent`
 WHERE `state` <> 'prepared' OR `recovery_state` <> 'ready' OR `recovery_attempts` <> 0 OR `recovery_retry_note` <> '';
DROP TABLE `ton_recovery_rollback_guard`;
ALTER TABLE `chain_send_intent`
 DROP CHECK `ck_chain_send_recovery_state`,
 DROP CHECK `ck_chain_send_recovery_counts`,
 DROP CHECK `ck_chain_send_recovery_lease`,
 DROP CHECK `ck_chain_send_recovery_receipt`,
 DROP INDEX `ix_chain_send_recovery_due`,
 DROP INDEX `ix_chain_send_recovery_lease`,
 DROP COLUMN `recovery_state`,
 DROP COLUMN `recovery_attempts`,
 DROP COLUMN `recovery_generation`,
 DROP COLUMN `recovery_next_at`,
 DROP COLUMN `recovery_lease_until`,
 DROP COLUMN `recovery_checked_at`,
 DROP COLUMN `recovery_error`,
 DROP COLUMN `recovery_receipt_hash`,
 DROP COLUMN `recovery_retry_note`;
