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

ALTER TABLE `chain_receive_address`
 ADD CONSTRAINT `ck_receive_route` CHECK (`route`='single_use' OR (`route`='ton_memo' AND `network`='ton-testnet'));

ALTER TABLE `chain_receive_request`
 ADD CONSTRAINT `fk_receive_address` FOREIGN KEY (`address_id`) REFERENCES `chain_receive_address` (`id`),
 ADD CONSTRAINT `fk_receive_event` FOREIGN KEY (`matched_event_id`) REFERENCES `chain_transfer_event` (`id`),
 ADD CONSTRAINT `ck_receive_terms` CHECK (`purpose` IN ('deposit','merchant_payment') AND `beneficiary_id`>0 AND `opens_at`>0 AND `expires_at`>`opens_at`);
