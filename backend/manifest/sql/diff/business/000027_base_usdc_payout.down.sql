CREATE TABLE `base_usdc_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `base_usdc_rollback_guard` SELECT COUNT(*) FROM `base_payout` WHERE `asset_id`<>'base-sepolia:eth';
DROP TABLE `base_usdc_rollback_guard`;
ALTER TABLE `base_payout` DROP CHECK `ck_base_payout_asset`, DROP COLUMN `transfer_log_index`, DROP COLUMN `token_atoms`, DROP COLUMN `asset_id`;
