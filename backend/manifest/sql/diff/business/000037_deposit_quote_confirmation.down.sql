CREATE TABLE `deposit_quote_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `deposit_quote_rollback_guard` SELECT COUNT(*) FROM `deposit_quote_confirmation`;
DROP TABLE `deposit_quote_rollback_guard`;
DROP TABLE `deposit_quote_confirmation`;
