CREATE TABLE `sol_deposit_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `sol_deposit_rollback_guard` SELECT COUNT(*) FROM `sol_deposit_order`;
DROP TABLE `sol_deposit_rollback_guard`;
DROP TABLE `sol_deposit_order`;
