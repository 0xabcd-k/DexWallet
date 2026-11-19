ALTER TABLE `chain_observed_transaction`
 MODIFY `fee_payer` VARCHAR(68) CHARACTER SET ascii COLLATE ascii_bin NOT NULL;
ALTER TABLE `chain_transfer_event`
 MODIFY `program_id` VARCHAR(68) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 MODIFY `mint` VARCHAR(68) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 MODIFY `source_account` VARCHAR(68) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 MODIFY `source_owner` VARCHAR(68) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 MODIFY `recipient_account` VARCHAR(68) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 MODIFY `recipient_owner` VARCHAR(68) CHARACTER SET ascii COLLATE ascii_bin NOT NULL;
