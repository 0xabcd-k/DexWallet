CREATE TABLE `base_payout_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `base_payout_rollback_guard` SELECT COUNT(*) FROM `base_payout`;
DROP TABLE `base_payout_rollback_guard`;
DROP TABLE `base_payout`;
