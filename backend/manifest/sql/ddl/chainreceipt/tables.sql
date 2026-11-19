CREATE TABLE `chain_observed_transaction` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `network` VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `signature` VARCHAR(88) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `slot` BIGINT NOT NULL,
    `fee_payer` VARCHAR(68) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `fee_atoms` VARCHAR(78) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `block_hash` VARCHAR(66) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    `execution_fee_atoms` VARCHAR(78) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    `l1_fee_atoms` VARCHAR(78) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    `operator_fee_atoms` VARCHAR(78) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    `fingerprint` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `evidence_json` MEDIUMTEXT NOT NULL,
    `call_trace_fingerprint` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    `call_trace_json` MEDIUMTEXT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_chain_observed_transaction` (`network`, `signature`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `chain_transfer_event` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `transaction_id` BIGINT NOT NULL,
    `event_kind` VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'solana_instruction',
    `instruction_index` INT NOT NULL,
    `asset_id` VARCHAR(180) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `program_id` VARCHAR(68) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `mint` VARCHAR(68) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `decimals` INT NOT NULL,
    `source_account` VARCHAR(68) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `source_owner` VARCHAR(68) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `recipient_account` VARCHAR(68) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `recipient_owner` VARCHAR(68) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `amount_atoms` VARCHAR(78) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_chain_transfer_event` (`transaction_id`, `event_kind`, `instruction_index`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
