CREATE TABLE `sol_usdc_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `sol_usdc_rollback_guard` SELECT COUNT(*) FROM `sol_deposit_order` WHERE `asset_id` <> 'solana-devnet:sol' OR LENGTH(`message_base64`)>512;
DROP TABLE `sol_usdc_rollback_guard`;
ALTER TABLE `sol_deposit_order` DROP CHECK `ck_sol_deposit_asset`, DROP CHECK `ck_sol_deposit_funding`,
 DROP COLUMN `asset_id`, DROP COLUMN `actual_funding_lamports`, MODIFY COLUMN `message_base64` VARCHAR(512) NOT NULL;
