CREATE TABLE `chain_send_intent` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `network` VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `request_key` VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `sender` VARCHAR(68) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `recipient` VARCHAR(68) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `asset_id` VARCHAR(180) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `amount_atoms` VARCHAR(39) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `public_key` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `subwallet_id` BIGINT NOT NULL,
    `seqno` BIGINT NOT NULL,
    `valid_until` BIGINT NOT NULL,
    `payload_hash` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `signed_boc` MEDIUMTEXT NULL,
    `message_hash` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    `body_hash` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    `state` VARCHAR(24) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `attempts` BIGINT NOT NULL DEFAULT 0,
    `last_error` TEXT NOT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_chain_send_request` (`network`, `request_key`),
    UNIQUE KEY `uk_chain_send_seqno` (`network`, `sender`, `seqno`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

ALTER TABLE `chain_send_intent`
 ADD CONSTRAINT `ck_chain_send_seqno` CHECK (`seqno` BETWEEN 1 AND 4294967295),
 ADD CONSTRAINT `ck_chain_send_subwallet` CHECK (`subwallet_id` BETWEEN 0 AND 4294967295),
 ADD CONSTRAINT `ck_chain_send_expiry` CHECK (`valid_until` BETWEEN 1 AND 4294967295),
 ADD CONSTRAINT `ck_chain_send_attempts` CHECK (`attempts` >= 0),
 ADD CONSTRAINT `ck_chain_send_state` CHECK (`state` IN ('prepared','signed','broadcast_unknown','submitted')),
 ADD CONSTRAINT `ck_chain_send_artifact` CHECK ((`state`='prepared' AND `signed_boc` IS NULL AND `message_hash`='' AND `body_hash`='') OR (`state`<>'prepared' AND `signed_boc` IS NOT NULL AND CHAR_LENGTH(`message_hash`)=64 AND CHAR_LENGTH(`body_hash`)=64));
