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

ALTER TABLE `ton_receive_cursor`
 ADD CONSTRAINT `fk_ton_receive_address` FOREIGN KEY (`address_id`) REFERENCES `chain_receive_address` (`id`);

ALTER TABLE `ton_receive_candidate`
 ADD CONSTRAINT `fk_ton_receive_cursor` FOREIGN KEY (`cursor_id`) REFERENCES `ton_receive_cursor` (`id`),
 ADD CONSTRAINT `ck_ton_receive_status` CHECK (`status` IN ('pending','matched','unmatched','ignored'));
