ALTER TABLE `sol_deposit_order`
 ADD COLUMN `asset_id` VARCHAR(32) NOT NULL DEFAULT 'solana-devnet:sol' AFTER `order_idx`,
 ADD COLUMN `actual_funding_lamports` BIGINT NOT NULL DEFAULT 0 AFTER `asset_id`,
 MODIFY COLUMN `message_base64` VARCHAR(1024) NOT NULL;
ALTER TABLE `sol_deposit_order`
 ADD CONSTRAINT `ck_sol_deposit_asset` CHECK (`asset_id` IN ('solana-devnet:sol','solana-devnet:usdc')),
 ADD CONSTRAINT `ck_sol_deposit_funding` CHECK (`actual_funding_lamports`=0 OR
   (`asset_id`='solana-devnet:usdc' AND `execution_state`='finalized' AND `actual_funding_lamports`>0));
