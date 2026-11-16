-- Evidence must be archived explicitly before dropping the receipt schema.
CREATE TABLE `chain_receipt_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used` = 0));
INSERT INTO `chain_receipt_rollback_guard` SELECT COUNT(*) FROM `chain_observed_transaction`;
DROP TABLE `chain_receipt_rollback_guard`;
DROP TABLE `chain_transfer_event`;
DROP TABLE `chain_observed_transaction`;
