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
 ADD CONSTRAINT `ck_sol_deposit_spl_asset` CHECK (`asset_id` IN ('solana-devnet:sol','solana-devnet:usdc') OR
   (LEFT(`asset_id`,19)='solana-devnet:usdt:' AND CHAR_LENGTH(`asset_id`) BETWEEN 51 AND 63)),
 ADD CONSTRAINT `ck_sol_deposit_spl_funding` CHECK (`actual_funding_lamports`=0 OR
   (`asset_id`<>'solana-devnet:sol' AND `execution_state`='finalized' AND `actual_funding_lamports`>0));

ALTER TABLE `base_deposit_order`
 ADD CONSTRAINT `ck_base_deposit_asset` CHECK (`asset_id` IN ('base-sepolia:usdc','base-sepolia:eth')),
 ADD CONSTRAINT `fk_base_deposit_quote` FOREIGN KEY (`quote_idx`) REFERENCES `deposit_quote_confirmation` (`quote_idx`),
 ADD CONSTRAINT `fk_base_deposit_voucher` FOREIGN KEY (`credit_voucher_id`) REFERENCES `ledger_voucher` (`id`),
 ADD CONSTRAINT `ck_base_deposit_wire` CHECK ((`tx_hash` IS NULL AND `sponsor_nonce` IS NULL AND `signed_wire`='' AND `state` IN ('accepted','review'))
   OR (`tx_hash` IS NOT NULL AND `sponsor_nonce` IS NOT NULL AND `signed_wire`<>'' AND `state` IN ('signed','review','credited'))),
 ADD CONSTRAINT `ck_base_deposit_credit` CHECK ((`credit_voucher_id` IS NULL AND `credited_usd` IS NULL AND `state`<>'credited')
   OR (`state`='credited' AND `credit_voucher_id` IS NOT NULL AND `credited_usd` IS NOT NULL AND `credited_usd`>0 AND `finalized_height`>0 AND `actual_fee_wei`<>''));

ALTER TABLE `tron_deposit_order`
 ADD CONSTRAINT `ck_tron_deposit_asset` CHECK (`asset_id` IN ('tron-nile:trx','tron-nile:usdt:TYGk3YPGFa7ZLhXfDBd5QzN9Ms9DmPC9tn')),
 ADD CONSTRAINT `ck_tron_deposit_energy` CHECK ((`actual_energy` IS NULL OR `actual_energy`>=0) AND (`state`<>'credited' OR (`asset_id`='tron-nile:trx' AND COALESCE(`actual_energy`,0)=0) OR (`asset_id`<>'tron-nile:trx' AND `actual_energy` IS NOT NULL AND `actual_energy`>0))),
 ADD CONSTRAINT `fk_tron_deposit_quote` FOREIGN KEY (`quote_idx`) REFERENCES `deposit_quote_confirmation` (`quote_idx`),
 ADD CONSTRAINT `fk_tron_deposit_voucher` FOREIGN KEY (`credit_voucher_id`) REFERENCES `ledger_voucher` (`id`),
 ADD CONSTRAINT `ck_tron_deposit_state` CHECK (`state` IN ('signed','review','credited')),
 ADD CONSTRAINT `ck_tron_deposit_credit` CHECK ((`state`='credited' AND `credit_voucher_id` IS NOT NULL AND `credited_usd` IS NOT NULL AND `credited_usd`>0 AND `actual_fee_sun` IS NOT NULL AND `actual_fee_sun`=0 AND `actual_bandwidth` IS NOT NULL AND `actual_bandwidth`>0 AND `finalized_height` IS NOT NULL AND `finalized_height`>0)
    OR (`state`<>'credited' AND `credit_voucher_id` IS NULL AND `credited_usd` IS NULL));

ALTER TABLE `ton_deposit_order`
 ADD CONSTRAINT `fk_ton_deposit_quote` FOREIGN KEY (`quote_idx`) REFERENCES `deposit_quote_confirmation` (`quote_idx`),
 ADD CONSTRAINT `fk_ton_deposit_voucher` FOREIGN KEY (`credit_voucher_id`) REFERENCES `ledger_voucher` (`id`),
 ADD CONSTRAINT `ck_ton_deposit_state` CHECK (`state` IN ('accepted','signed','credited','review')),
 ADD CONSTRAINT `ck_ton_deposit_failure` CHECK (execution_failed=0 OR (execution_failed=1 AND state='review' AND dispatch_hash IS NOT NULL)),
 ADD CONSTRAINT `ck_ton_deposit_dispatch` CHECK ((`platform_signature` IS NULL AND `dispatch_hash` IS NULL AND `state` IN ('accepted','review')) OR (`platform_signature` IS NOT NULL AND `dispatch_hash` IS NOT NULL AND `state` IN ('signed','review','credited'))),
 ADD CONSTRAINT `ck_ton_deposit_credit` CHECK ((`state`='credited' AND `receive_hash` IS NOT NULL AND `actual_fee_nano` IS NOT NULL AND `actual_fee_nano`>0 AND `credit_voucher_id` IS NOT NULL AND `credited_usd` IS NOT NULL AND `credited_usd`>0) OR (`state`<>'credited' AND `credit_voucher_id` IS NULL AND `credited_usd` IS NULL));
