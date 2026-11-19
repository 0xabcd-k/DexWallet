ALTER TABLE `chain_observed_transaction`
 MODIFY `fee_atoms` VARCHAR(78) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 ADD `block_hash` VARCHAR(66) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '' AFTER `fee_atoms`,
 ADD `execution_fee_atoms` VARCHAR(78) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '' AFTER `block_hash`,
 ADD `l1_fee_atoms` VARCHAR(78) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '' AFTER `execution_fee_atoms`,
 ADD `operator_fee_atoms` VARCHAR(78) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '' AFTER `l1_fee_atoms`;
ALTER TABLE `chain_transfer_event`
 ADD `event_kind` VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'solana_instruction' AFTER `transaction_id`,
 MODIFY `amount_atoms` VARCHAR(78) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 DROP INDEX `uk_chain_transfer_event`,
 ADD UNIQUE KEY `uk_chain_transfer_event` (`transaction_id`, `event_kind`, `instruction_index`);
