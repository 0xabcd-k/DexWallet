CREATE TABLE `sol_token_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `sol_token_rollback_guard` SELECT COUNT(*) FROM `solana_payout` WHERE `asset_id`<>'solana-devnet:sol';
DROP TABLE `sol_token_rollback_guard`;
ALTER TABLE `solana_payout` DROP CHECK `ck_sol_payout_asset`, DROP CHECK `ck_sol_payout_funding`,
 DROP COLUMN `ata_funding_lamports`, DROP COLUMN `create_ata`, DROP COLUMN `token_atoms`, DROP COLUMN `mint`, DROP COLUMN `asset_id`;
