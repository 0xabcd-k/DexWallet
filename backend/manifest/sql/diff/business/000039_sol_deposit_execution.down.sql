CREATE TABLE `sol_execution_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `sol_execution_rollback_guard` SELECT COUNT(*) FROM `sol_deposit_order` WHERE `execution_state` <> 'waiting';
DROP TABLE `sol_execution_rollback_guard`;
ALTER TABLE `sol_deposit_order`
 DROP CHECK `ck_sol_deposit_execution`, DROP INDEX `uk_sol_deposit_tx`,
 DROP COLUMN `execution_state`, DROP COLUMN `sponsor_policy`, DROP COLUMN `tx_signature`, DROP COLUMN `actual_lamports_fee`, DROP COLUMN `finalized_slot`;
