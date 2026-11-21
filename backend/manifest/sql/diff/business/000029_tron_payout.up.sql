CREATE TABLE `tron_payout` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `request_key` VARCHAR(64) NOT NULL,
    `network` VARCHAR(32) NOT NULL,
    `sender` CHAR(34) NOT NULL,
    `recipient` CHAR(34) NOT NULL,
    `sun` BIGINT NOT NULL,
    `ref_block_id` CHAR(64) NOT NULL,
    `ref_height` BIGINT NOT NULL,
    `timestamp_ms` BIGINT NOT NULL,
    `expiration_ms` BIGINT NOT NULL,
    `raw_hex` VARCHAR(512) NOT NULL,
    `tx_id` CHAR(64) NOT NULL,
    `signature` VARCHAR(130) NOT NULL DEFAULT '',
    `state` VARCHAR(24) NOT NULL,
    `attempts` BIGINT NOT NULL DEFAULT 0,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_tron_payout_request` (`request_key`),
    UNIQUE KEY `uk_tron_payout_tx` (`tx_id`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;

ALTER TABLE `tron_payout`
 ADD CONSTRAINT `ck_tron_payout_source` CHECK (`network`='tron-nile' AND `sun`>0 AND `ref_height`>0 AND `expiration_ms`>`timestamp_ms` AND `timestamp_ms`>0 AND `attempts`>=0),
 ADD CONSTRAINT `ck_tron_payout_auth` CHECK ((`state`='prepared' AND `signature`='' AND `attempts`=0) OR (`state` IN ('signed','submitted','confirmed') AND CHAR_LENGTH(`signature`)=130));
