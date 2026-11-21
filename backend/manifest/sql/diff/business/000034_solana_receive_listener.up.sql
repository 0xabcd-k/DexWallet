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

ALTER TABLE `solana_receive_cursor`
 ADD CONSTRAINT `ck_sol_receive_cursor` CHECK (`id`=1 AND `last_slot`>0);

ALTER TABLE `solana_receive_candidate`
 ADD CONSTRAINT `fk_sol_receive_request` FOREIGN KEY (`request_id`) REFERENCES `chain_receive_request` (`id`),
 ADD CONSTRAINT `ck_sol_receive_status` CHECK (`status` IN ('pending','matched','unmatched','ignored'));
