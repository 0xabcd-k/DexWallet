CREATE TABLE `ton_deposit_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `ton_deposit_rollback_guard` SELECT COUNT(*) FROM `ton_deposit_order`;
DROP TABLE `ton_deposit_rollback_guard`;
DROP TABLE `ton_deposit_order`;
