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

ALTER TABLE `base_receive_cursor`
 ADD CONSTRAINT `ck_base_receive_cursor` CHECK (`height`>0 AND `asset_id` IN ('base-sepolia:eth','base-sepolia:usdc'));

ALTER TABLE `base_receive_candidate`
 ADD CONSTRAINT `fk_base_receive_request` FOREIGN KEY (`request_id`) REFERENCES `chain_receive_request` (`id`),
 ADD CONSTRAINT `ck_base_receive_status` CHECK (`status` IN ('pending','matched','unmatched')),
 ADD CONSTRAINT `ck_base_receive_position` CHECK ((`event_kind`='evm_value' AND `event_index`=0) OR (`event_kind`='evm_log' AND `event_index`>=0) OR (`event_kind`='evm_call' AND `event_index`>0));

ALTER TABLE `chain_receive_request` ADD KEY `idx_receive_asset_open` (`asset_id`,`opens_at`);
