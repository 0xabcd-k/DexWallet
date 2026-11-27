CREATE TABLE `deposit_quote_confirmation` (
    `quote_idx` CHAR(32) NOT NULL,
    `user_id` BIGINT NOT NULL,
    `snapshot_json` TEXT NOT NULL,
    `quote_expires_at` DATETIME(3) NOT NULL,
    `confirmed_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`quote_idx`),
    KEY `ix_deposit_quote_user` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;

CREATE TABLE `sol_deposit_order` (
    `quote_idx` CHAR(32) NOT NULL,
    `order_idx` CHAR(32) NOT NULL,
    `message_base64` VARCHAR(512) NOT NULL,
    `message_sha256` CHAR(64) NOT NULL,
    `last_valid_height` BIGINT NOT NULL,
    `review_after` DATETIME(3) NOT NULL,
    `user_signature` VARCHAR(88) NOT NULL DEFAULT '',
    `accepted_at` DATETIME(3) NULL,
    `execution_state` VARCHAR(16) NOT NULL DEFAULT 'waiting',
    `sponsor_policy` VARCHAR(48) NOT NULL DEFAULT '',
    `tx_signature` VARCHAR(88) NULL,
    `actual_lamports_fee` BIGINT NOT NULL DEFAULT 0,
    `finalized_slot` BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (`quote_idx`),
    UNIQUE KEY `uk_sol_deposit_tx` (`tx_signature`),
    UNIQUE KEY `uk_sol_deposit_order` (`order_idx`),
    UNIQUE KEY `uk_sol_deposit_message` (`message_sha256`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;
