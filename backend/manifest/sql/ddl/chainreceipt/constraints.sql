ALTER TABLE `chain_transfer_event`
 ADD CONSTRAINT `fk_chain_event_transaction` FOREIGN KEY (`transaction_id`) REFERENCES `chain_observed_transaction` (`id`),
 ADD CONSTRAINT `ck_chain_event_index` CHECK (`instruction_index` >= 0),
 ADD CONSTRAINT `ck_chain_event_decimals` CHECK (`decimals` BETWEEN 0 AND 18);

ALTER TABLE `chain_receive_address`
 ADD CONSTRAINT `ck_receive_route` CHECK (`route`='single_use' OR (`route`='ton_memo' AND `network`='ton-testnet'));

ALTER TABLE `chain_receive_request`
 ADD CONSTRAINT `fk_receive_address` FOREIGN KEY (`address_id`) REFERENCES `chain_receive_address` (`id`),
 ADD CONSTRAINT `fk_receive_event` FOREIGN KEY (`matched_event_id`) REFERENCES `chain_transfer_event` (`id`),
 ADD CONSTRAINT `ck_receive_terms` CHECK (`purpose` IN ('deposit','merchant_payment') AND `beneficiary_id`>0 AND `opens_at`>0 AND `expires_at`>`opens_at`);

ALTER TABLE `ton_receive_cursor`
 ADD CONSTRAINT `fk_ton_receive_address` FOREIGN KEY (`address_id`) REFERENCES `chain_receive_address` (`id`);

ALTER TABLE `ton_receive_candidate`
 ADD CONSTRAINT `fk_ton_receive_cursor` FOREIGN KEY (`cursor_id`) REFERENCES `ton_receive_cursor` (`id`),
 ADD CONSTRAINT `ck_ton_receive_status` CHECK (`status` IN ('pending','matched','unmatched','ignored'));

ALTER TABLE `solana_receive_cursor`
 ADD CONSTRAINT `ck_sol_receive_cursor` CHECK (`id`=1 AND `last_slot`>0);

ALTER TABLE `solana_receive_candidate`
 ADD CONSTRAINT `fk_sol_receive_request` FOREIGN KEY (`request_id`) REFERENCES `chain_receive_request` (`id`),
 ADD CONSTRAINT `ck_sol_receive_status` CHECK (`status` IN ('pending','matched','unmatched','ignored'));

ALTER TABLE `base_receive_cursor`
 ADD CONSTRAINT `ck_base_receive_cursor` CHECK (`height`>0 AND `asset_id` IN ('base-sepolia:eth','base-sepolia:usdc'));

ALTER TABLE `base_receive_candidate`
 ADD CONSTRAINT `fk_base_receive_request` FOREIGN KEY (`request_id`) REFERENCES `chain_receive_request` (`id`),
 ADD CONSTRAINT `ck_base_receive_status` CHECK (`status` IN ('pending','matched','unmatched')),
 ADD CONSTRAINT `ck_base_receive_position` CHECK ((`event_kind`='evm_value' AND `event_index`=0) OR (`event_kind`='evm_log' AND `event_index`>=0) OR (`event_kind`='evm_call' AND `event_index`>0));

ALTER TABLE `chain_receive_request` ADD KEY `idx_receive_asset_open` (`asset_id`,`opens_at`);
