CREATE TABLE `tron_deposit_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `tron_deposit_rollback_guard` SELECT COUNT(*) FROM `tron_deposit_order`;
DROP TABLE `tron_deposit_rollback_guard`;
DROP TABLE `tron_deposit_order`;
