ALTER TABLE `sol_deposit_order`
 DROP CHECK `ck_sol_deposit_asset`, DROP CHECK `ck_sol_deposit_funding`,
 MODIFY COLUMN `asset_id` VARCHAR(96) NOT NULL DEFAULT 'solana-devnet:sol',
 ADD CONSTRAINT `ck_sol_deposit_spl_asset` CHECK (`asset_id` IN ('solana-devnet:sol','solana-devnet:usdc') OR
   (LEFT(`asset_id`,19)='solana-devnet:usdt:' AND CHAR_LENGTH(`asset_id`) BETWEEN 51 AND 63)),
 ADD CONSTRAINT `ck_sol_deposit_spl_funding` CHECK (`actual_funding_lamports`=0 OR
   (`asset_id`<>'solana-devnet:sol' AND `execution_state`='finalized' AND `actual_funding_lamports`>0));
