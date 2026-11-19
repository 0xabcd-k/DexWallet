-- Preserve Base evidence and amounts that business 16 cannot represent.
CREATE TABLE `base_receipt_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used` = 0));
INSERT INTO `base_receipt_rollback_guard`
 SELECT (SELECT COUNT(*) FROM `chain_observed_transaction` WHERE `block_hash` <> '' OR `execution_fee_atoms` <> '' OR `l1_fee_atoms` <> '' OR `operator_fee_atoms` <> '' OR CHAR_LENGTH(`fee_atoms`) > 20)
      + (SELECT COUNT(*) FROM `chain_transfer_event` WHERE `event_kind` <> 'solana_instruction' OR CHAR_LENGTH(`amount_atoms`) > 20);
DROP TABLE `base_receipt_rollback_guard`;
ALTER TABLE `chain_transfer_event`
 DROP INDEX `uk_chain_transfer_event`,
 DROP COLUMN `event_kind`,
 MODIFY `amount_atoms` VARCHAR(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 ADD UNIQUE KEY `uk_chain_transfer_event` (`transaction_id`, `instruction_index`);
ALTER TABLE `chain_observed_transaction`
 DROP COLUMN `block_hash`, DROP COLUMN `execution_fee_atoms`, DROP COLUMN `l1_fee_atoms`, DROP COLUMN `operator_fee_atoms`,
 MODIFY `fee_atoms` VARCHAR(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL;
