ALTER TABLE `deposit_quote_confirmation`
 ADD CONSTRAINT `fk_deposit_quote_user` FOREIGN KEY (`user_id`) REFERENCES `user` (`id`),
 ADD CONSTRAINT `ck_deposit_quote_window` CHECK (`confirmed_at` < `quote_expires_at`);

ALTER TABLE `sol_deposit_order`
 ADD CONSTRAINT `fk_sol_deposit_quote` FOREIGN KEY (`quote_idx`) REFERENCES `deposit_quote_confirmation` (`quote_idx`),
 ADD CONSTRAINT `ck_sol_deposit_height` CHECK (`last_valid_height` > 0),
 ADD CONSTRAINT `ck_sol_deposit_signature` CHECK ((`user_signature` = '' AND `accepted_at` IS NULL) OR (`user_signature` <> '' AND `accepted_at` IS NOT NULL));

ALTER TABLE `sol_deposit_order`
 ADD CONSTRAINT `ck_sol_deposit_execution` CHECK (
   (`execution_state` = 'waiting' AND `tx_signature` IS NULL OR
    `execution_state` = 'review' AND `accepted_at` IS NOT NULL OR
    `execution_state` IN ('signed','submitted','finalized','failed') AND `accepted_at` IS NOT NULL AND `tx_signature` IS NOT NULL)
   AND ((`tx_signature` IS NULL AND `sponsor_policy` = '') OR (`tx_signature` IS NOT NULL AND `sponsor_policy` <> ''))
   AND ((`execution_state` IN ('finalized','failed') AND `finalized_slot` > 0 AND `actual_lamports_fee` > 0)
     OR (`execution_state` NOT IN ('finalized','failed') AND `finalized_slot` = 0 AND `actual_lamports_fee` = 0))
 );

ALTER TABLE `sol_deposit_order`
 ADD CONSTRAINT `fk_sol_credit_voucher` FOREIGN KEY (`credit_voucher_id`) REFERENCES `ledger_voucher` (`id`),
 ADD CONSTRAINT `ck_sol_deposit_credit` CHECK (
   (`credit_voucher_id` IS NULL AND `credited_usd` IS NULL AND `credited_at` IS NULL)
   OR (`execution_state`='finalized' AND `credit_voucher_id` IS NOT NULL AND `credited_usd` IS NOT NULL
       AND `credited_usd` > 0 AND `credited_usd` = TRUNCATE(`credited_usd`,2) AND `credited_at` IS NOT NULL)
 );

ALTER TABLE `sol_deposit_order`
 ADD CONSTRAINT `ck_sol_deposit_asset` CHECK (`asset_id` IN ('solana-devnet:sol','solana-devnet:usdc')),
 ADD CONSTRAINT `ck_sol_deposit_funding` CHECK (`actual_funding_lamports`=0 OR
   (`asset_id`='solana-devnet:usdc' AND `execution_state`='finalized' AND `actual_funding_lamports`>0));
