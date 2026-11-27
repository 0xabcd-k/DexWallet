CREATE TABLE `sol_credit_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `sol_credit_rollback_guard` SELECT COUNT(*) FROM `sol_deposit_order` WHERE `credit_voucher_id` IS NOT NULL;
DROP TABLE `sol_credit_rollback_guard`;
ALTER TABLE `sol_deposit_order`
 DROP FOREIGN KEY `fk_sol_credit_voucher`, DROP CHECK `ck_sol_deposit_credit`,
 DROP INDEX `uk_sol_credit_voucher`, DROP PRIMARY KEY, ADD PRIMARY KEY (`quote_idx`),
 DROP INDEX `uk_sol_deposit_quote`, DROP COLUMN `id`,
 DROP COLUMN `credit_voucher_id`, DROP COLUMN `credited_usd`, DROP COLUMN `credited_at`;
