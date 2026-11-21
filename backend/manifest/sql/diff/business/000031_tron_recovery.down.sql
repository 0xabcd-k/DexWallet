-- Preserve outstanding signatures and all operator/worker history.
CREATE TABLE `tron_recovery_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `tron_recovery_rollback_guard` SELECT COUNT(*) FROM `tron_payout`
 WHERE `state` IN ('signed','submitted') OR `recovery_generation` <> 0;
DROP TABLE `tron_recovery_rollback_guard`;
ALTER TABLE `tron_payout`
 DROP CHECK `ck_tron_recovery_state`,
 DROP CHECK `ck_tron_recovery_counts`,
 DROP CHECK `ck_tron_recovery_lease`,
 DROP INDEX `ix_tron_recovery_due`,
 DROP INDEX `ix_tron_recovery_lease`,
 DROP COLUMN `recovery_state`,
 DROP COLUMN `recovery_attempts`,
 DROP COLUMN `recovery_generation`,
 DROP COLUMN `recovery_next_at`,
 DROP COLUMN `recovery_lease_until`,
 DROP COLUMN `recovery_checked_at`,
 DROP COLUMN `recovery_error`,
 DROP COLUMN `recovery_retry_note`;
