ALTER TABLE `solana_payout`
 ADD COLUMN `asset_id` VARCHAR(128) NOT NULL DEFAULT 'solana-devnet:sol' AFTER `lamports`,
 ADD COLUMN `mint` VARCHAR(44) NOT NULL DEFAULT '' AFTER `asset_id`,
 ADD COLUMN `token_atoms` VARCHAR(20) NOT NULL DEFAULT '' AFTER `mint`,
 ADD COLUMN `create_ata` TINYINT NOT NULL DEFAULT 0 AFTER `token_atoms`,
 ADD COLUMN `ata_funding_lamports` BIGINT NOT NULL DEFAULT 0 AFTER `create_ata`;
ALTER TABLE `solana_payout`
 ADD CONSTRAINT `ck_sol_payout_asset` CHECK ((`asset_id`='solana-devnet:sol' AND `mint`='' AND `token_atoms`='' AND `create_ata`=0) OR (`asset_id`<>'solana-devnet:sol' AND `mint`<>'' AND `token_atoms`<>'' AND `lamports`='0' AND `create_ata` IN (0,1))),
 ADD CONSTRAINT `ck_sol_payout_funding` CHECK (`ata_funding_lamports`=0 OR (`ata_funding_lamports`>0 AND `create_ata`=1 AND `state`='confirmed'));
