CREATE TABLE `solana_payout` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `request_key` VARCHAR(64) NOT NULL,
    `sender` VARCHAR(44) NOT NULL,
    `recipient` VARCHAR(44) NOT NULL,
    `lamports` VARCHAR(20) NOT NULL,
    `blockhash` VARCHAR(44) NOT NULL,
    `last_valid_height` BIGINT NOT NULL,
    `message_hash` CHAR(64) NOT NULL,
    `signature` VARCHAR(88) NOT NULL DEFAULT '',
    `state` VARCHAR(24) NOT NULL,
    `attempts` BIGINT NOT NULL DEFAULT 0,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_sol_payout_key` (`request_key`),
    UNIQUE KEY `uk_sol_payout_message` (`message_hash`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;

ALTER TABLE `solana_payout`
 ADD CONSTRAINT `ck_sol_payout_height` CHECK (`last_valid_height` > 0 AND `attempts` >= 0),
 ADD CONSTRAINT `ck_sol_payout_state` CHECK ((`state` = 'prepared' AND `signature` = '' AND `attempts` = 0) OR (`state` IN ('signed','submitted','confirmed') AND CHAR_LENGTH(`signature`) BETWEEN 64 AND 88));
