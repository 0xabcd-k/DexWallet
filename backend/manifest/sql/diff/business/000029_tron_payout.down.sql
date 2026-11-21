CREATE TABLE `tron_payout_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `tron_payout_rollback_guard` SELECT COUNT(*) FROM `tron_payout`;
DROP TABLE `tron_payout_rollback_guard`;
DROP TABLE `tron_payout`;
