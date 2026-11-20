CREATE TABLE `base_payout` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `request_key` VARCHAR(64) NOT NULL,
    `chain_id` BIGINT NOT NULL,
    `sender` CHAR(42) NOT NULL,
    `recipient` CHAR(42) NOT NULL,
    `wei_amount` VARCHAR(78) NOT NULL,
    `nonce` VARCHAR(20) NOT NULL,
    `gas_limit` BIGINT NOT NULL,
    `fee_cap` VARCHAR(78) NOT NULL,
    `tip_cap` VARCHAR(78) NOT NULL,
    `signing_hash` CHAR(66) NOT NULL,
    `tx_hash` VARCHAR(66) NOT NULL DEFAULT '',
    `signed_tx` VARCHAR(512) NOT NULL DEFAULT '',
    `state` VARCHAR(24) NOT NULL,
    `attempts` BIGINT NOT NULL DEFAULT 0,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_base_payout_request` (`request_key`),
    UNIQUE KEY `uk_base_payout_nonce` (`sender`, `nonce`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;

ALTER TABLE `base_payout`
 ADD CONSTRAINT `ck_base_payout_chain` CHECK (`chain_id`=84532 AND `gas_limit`>=21000 AND `attempts`>=0),
 ADD CONSTRAINT `ck_base_payout_auth` CHECK ((`state`='prepared' AND `signed_tx`='' AND `tx_hash`='' AND `attempts`=0) OR (`state` IN ('signed','submitted','confirmed') AND CHAR_LENGTH(`tx_hash`)=66 AND `signed_tx`<>''));
