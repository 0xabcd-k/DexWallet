CREATE TABLE `base_deposit_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `base_deposit_rollback_guard` SELECT COUNT(*) FROM `base_deposit_order`;
DROP TABLE `base_deposit_rollback_guard`;
DROP TABLE `base_deposit_order`;
