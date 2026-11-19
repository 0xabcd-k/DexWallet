-- Do not truncate TON addresses or erase the ability to recover their facts.
CREATE TABLE `ton_receipt_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used` = 0));
INSERT INTO `ton_receipt_rollback_guard`
 SELECT (SELECT COUNT(*) FROM `chain_observed_transaction` WHERE CHAR_LENGTH(`fee_payer`) > 44)
      + (SELECT COUNT(*) FROM `chain_transfer_event` WHERE CHAR_LENGTH(`program_id`) > 44 OR CHAR_LENGTH(`mint`) > 44 OR CHAR_LENGTH(`source_account`) > 44 OR CHAR_LENGTH(`source_owner`) > 44 OR CHAR_LENGTH(`recipient_account`) > 44 OR CHAR_LENGTH(`recipient_owner`) > 44);
DROP TABLE `ton_receipt_rollback_guard`;
ALTER TABLE `chain_transfer_event`
 MODIFY `program_id` VARCHAR(44) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 MODIFY `mint` VARCHAR(44) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 MODIFY `source_account` VARCHAR(44) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 MODIFY `source_owner` VARCHAR(44) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 MODIFY `recipient_account` VARCHAR(44) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 MODIFY `recipient_owner` VARCHAR(44) CHARACTER SET ascii COLLATE ascii_bin NOT NULL;
ALTER TABLE `chain_observed_transaction`
 MODIFY `fee_payer` VARCHAR(44) CHARACTER SET ascii COLLATE ascii_bin NOT NULL;
