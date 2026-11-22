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

CREATE TABLE `chain_receive_address` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `network` VARCHAR(32) NOT NULL,
    `owner` VARCHAR(68) NOT NULL,
    `route` VARCHAR(16) NOT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_receive_owner` (`network`,`owner`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;

CREATE TABLE `chain_receive_request` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `request_key` VARCHAR(64) NOT NULL,
    `address_id` BIGINT NOT NULL,
    `payment_reference` VARCHAR(35) NOT NULL,
    `purpose` VARCHAR(20) NOT NULL,
    `beneficiary_id` BIGINT NOT NULL,
    `payer_owner` VARCHAR(68) NOT NULL,
    `asset_id` VARCHAR(180) NOT NULL,
    `amount_atoms` VARCHAR(78) NOT NULL,
    `opens_at` BIGINT NOT NULL,
    `expires_at` BIGINT NOT NULL,
    `matched_event_id` BIGINT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_receive_request` (`request_key`),
    UNIQUE KEY `uk_receive_route` (`address_id`,`payment_reference`),
    UNIQUE KEY `uk_receive_event` (`matched_event_id`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;

CREATE TABLE `ton_receive_cursor` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `address_id` BIGINT NOT NULL,
    `asset_id` VARCHAR(180) NOT NULL,
    `starts_at` BIGINT NOT NULL,
    `scan_account` VARCHAR(68) NOT NULL DEFAULT '',
    `last_lt` BIGINT NOT NULL DEFAULT 0,
    `last_hash` VARCHAR(64) NOT NULL DEFAULT '',
    `next_poll` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_ton_receive_stream` (`address_id`,`asset_id`),
    KEY `idx_ton_receive_poll` (`next_poll`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;

CREATE TABLE `ton_receive_candidate` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `cursor_id` BIGINT NOT NULL,
    `tx_lt` BIGINT NOT NULL,
    `tx_hash` CHAR(64) NOT NULL,
    `tx_time` BIGINT NOT NULL,
    `status` VARCHAR(16) NOT NULL DEFAULT 'pending',
    `attempts` BIGINT NOT NULL DEFAULT 0,
    `next_try` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `detail` VARCHAR(512) NOT NULL DEFAULT '',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_ton_receive_lt` (`cursor_id`,`tx_lt`),
    KEY `idx_ton_receive_due` (`status`,`next_try`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;

CREATE TABLE `solana_receive_cursor` (
    `id` BIGINT NOT NULL,
    `last_slot` BIGINT NOT NULL,
    `last_hash` VARCHAR(44) NOT NULL,
    PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;

CREATE TABLE `solana_receive_candidate` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `request_id` BIGINT NOT NULL,
    `signature` VARCHAR(88) NOT NULL,
    `slot` BIGINT NOT NULL,
    `block_time` BIGINT NOT NULL,
    `status` VARCHAR(16) NOT NULL DEFAULT 'pending',
    `matched_events` INT NOT NULL DEFAULT 0,
    `unmatched_events` INT NOT NULL DEFAULT 0,
    `attempts` BIGINT NOT NULL DEFAULT 0,
    `next_try` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `detail` VARCHAR(512) CHARACTER SET utf8mb4 NOT NULL DEFAULT '',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_sol_receive_request_tx` (`request_id`,`signature`),
    KEY `idx_sol_receive_due` (`status`,`next_try`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;

CREATE TABLE `base_receive_cursor` (
    `asset_id` VARCHAR(64) NOT NULL,
    `height` BIGINT NOT NULL,
    `block_hash` CHAR(66) NOT NULL,
    PRIMARY KEY (`asset_id`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;

CREATE TABLE `base_receive_candidate` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `request_id` BIGINT NOT NULL,
    `tx_hash` CHAR(66) NOT NULL,
    `event_kind` VARCHAR(16) NOT NULL,
    `event_index` INT NOT NULL,
    `amount_atoms` VARCHAR(78) NOT NULL,
    `height` BIGINT NOT NULL,
    `block_hash` CHAR(66) NOT NULL,
    `block_time` BIGINT NOT NULL,
    `status` VARCHAR(16) NOT NULL DEFAULT 'pending',
    `attempts` BIGINT NOT NULL DEFAULT 0,
    `next_try` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `detail` VARCHAR(512) CHARACTER SET utf8mb4 NOT NULL DEFAULT '',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_base_receive_event` (`tx_hash`,`event_kind`,`event_index`),
    KEY `idx_base_receive_due` (`status`,`next_try`,`id`),
    KEY `idx_base_receive_request` (`request_id`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;

CREATE TABLE `tron_receive_cursor` (
    `asset_id` VARCHAR(180) NOT NULL,
    `height` BIGINT NOT NULL,
    `block_hash` CHAR(64) NOT NULL,
    PRIMARY KEY (`asset_id`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;
