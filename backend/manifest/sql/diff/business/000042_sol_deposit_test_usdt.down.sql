CREATE TABLE `sol_test_usdt_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `sol_test_usdt_rollback_guard` SELECT COUNT(*) FROM `sol_deposit_order` WHERE `asset_id` NOT IN ('solana-devnet:sol','solana-devnet:usdc');
DROP TABLE `sol_test_usdt_rollback_guard`;
ALTER TABLE `sol_deposit_order`
 DROP CHECK `ck_sol_deposit_spl_asset`, DROP CHECK `ck_sol_deposit_spl_funding`,
 MODIFY COLUMN `asset_id` VARCHAR(32) NOT NULL DEFAULT 'solana-devnet:sol',
 ADD CONSTRAINT `ck_sol_deposit_asset` CHECK (`asset_id` IN ('solana-devnet:sol','solana-devnet:usdc')),
 ADD CONSTRAINT `ck_sol_deposit_funding` CHECK (`actual_funding_lamports`=0 OR
   (`asset_id`='solana-devnet:usdc' AND `execution_state`='finalized' AND `actual_funding_lamports`>0));
