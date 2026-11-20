ALTER TABLE `chain_send_intent`
 ADD CONSTRAINT `ck_chain_send_jetton_json` CHECK (`jetton_json` = '' OR JSON_VALID(`jetton_json`)),
 ADD CONSTRAINT `ck_chain_send_seqno` CHECK (`seqno` BETWEEN 1 AND 4294967295),
 ADD CONSTRAINT `ck_chain_send_subwallet` CHECK (`subwallet_id` BETWEEN 0 AND 4294967295),
 ADD CONSTRAINT `ck_chain_send_expiry` CHECK (`valid_until` BETWEEN 1 AND 4294967295),
 ADD CONSTRAINT `ck_chain_send_attempts` CHECK (`attempts` >= 0),
 ADD CONSTRAINT `ck_chain_send_state` CHECK (`state` IN ('prepared','signed','broadcast_unknown','submitted')),
 ADD CONSTRAINT `ck_chain_send_artifact` CHECK ((`state`='prepared' AND `signed_boc` IS NULL AND `message_hash`='' AND `body_hash`='') OR (`state`<>'prepared' AND `signed_boc` IS NOT NULL AND CHAR_LENGTH(`message_hash`)=64 AND CHAR_LENGTH(`body_hash`)=64));

ALTER TABLE `chain_send_intent`
 ADD CONSTRAINT `ck_chain_send_recovery_state` CHECK (`recovery_state` IN ('ready','running','confirmed','review')),
 ADD CONSTRAINT `ck_chain_send_recovery_counts` CHECK (`recovery_attempts` >= 0 AND `recovery_generation` >= `recovery_attempts`),
 ADD CONSTRAINT `ck_chain_send_recovery_lease` CHECK ((`recovery_state` = 'running' AND `recovery_lease_until` IS NOT NULL) OR (`recovery_state` <> 'running' AND `recovery_lease_until` IS NULL)),
 ADD CONSTRAINT `ck_chain_send_recovery_receipt` CHECK ((`recovery_state` = 'confirmed' AND CHAR_LENGTH(`recovery_receipt_hash`) = 64 AND `recovery_checked_at` IS NOT NULL) OR (`recovery_state` <> 'confirmed' AND `recovery_receipt_hash` = ''));

ALTER TABLE `solana_payout`
 ADD CONSTRAINT `ck_sol_payout_height` CHECK (`last_valid_height` > 0 AND `attempts` >= 0),
 ADD CONSTRAINT `ck_sol_payout_state` CHECK ((`state` = 'prepared' AND `signature` = '' AND `attempts` = 0) OR (`state` IN ('signed','submitted','confirmed') AND CHAR_LENGTH(`signature`) BETWEEN 64 AND 88));

ALTER TABLE `solana_payout`
 ADD CONSTRAINT `ck_sol_payout_asset` CHECK ((`asset_id`='solana-devnet:sol' AND `mint`='' AND `token_atoms`='' AND `create_ata`=0) OR (`asset_id`<>'solana-devnet:sol' AND `mint`<>'' AND `token_atoms`<>'' AND `lamports`='0' AND `create_ata` IN (0,1))),
 ADD CONSTRAINT `ck_sol_payout_funding` CHECK (`ata_funding_lamports`=0 OR (`ata_funding_lamports`>0 AND `create_ata`=1 AND `state`='confirmed'));

ALTER TABLE `solana_payout`
 ADD CONSTRAINT `ck_sol_recovery_state` CHECK (`recovery_state` IN ('ready','running','review','confirmed') AND ((`state`='confirmed')=(`recovery_state`='confirmed'))),
 ADD CONSTRAINT `ck_sol_recovery_counts` CHECK (`recovery_attempts` >= 0 AND `recovery_generation` >= `recovery_attempts`),
 ADD CONSTRAINT `ck_sol_recovery_lease` CHECK ((`recovery_state`='running')=(`recovery_lease_until` IS NOT NULL));

ALTER TABLE `base_payout`
 ADD CONSTRAINT `ck_base_payout_chain` CHECK (`chain_id`=84532 AND `gas_limit`>=21000 AND `attempts`>=0),
 ADD CONSTRAINT `ck_base_payout_auth` CHECK ((`state`='prepared' AND `signed_tx`='' AND `tx_hash`='' AND `attempts`=0) OR (`state` IN ('signed','submitted','confirmed') AND CHAR_LENGTH(`tx_hash`)=66 AND `signed_tx`<>''));

ALTER TABLE `base_payout`
 ADD CONSTRAINT `ck_base_payout_asset` CHECK (
    (`asset_id`='base-sepolia:eth' AND `token_atoms`='' AND `transfer_log_index` IS NULL)
    OR (`asset_id`='base-sepolia:usdc' AND `wei_amount`='0' AND CHAR_LENGTH(`token_atoms`)>0
        AND ((`state`='confirmed' AND `transfer_log_index` IS NOT NULL AND `transfer_log_index` BETWEEN 0 AND 2147483647)
             OR (`state`<>'confirmed' AND `transfer_log_index` IS NULL))));

ALTER TABLE `base_payout`
 ADD CONSTRAINT `ck_base_recovery_state` CHECK (`recovery_state` IN ('ready','running','review','confirmed') AND ((`state`='confirmed')=(`recovery_state`='confirmed'))),
 ADD CONSTRAINT `ck_base_recovery_counts` CHECK (`recovery_attempts` >= 0 AND `recovery_generation` >= `recovery_attempts`),
 ADD CONSTRAINT `ck_base_recovery_lease` CHECK ((`recovery_state`='running')=(`recovery_lease_until` IS NOT NULL));
