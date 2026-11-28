CREATE TABLE `base_deposit_order` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `quote_idx` CHAR(32) NOT NULL,
    `user_signature` CHAR(132) NOT NULL,
    `accepted_at` DATETIME(3) NOT NULL,
    `state` VARCHAR(16) NOT NULL DEFAULT 'accepted',
    `sponsor` CHAR(42) NOT NULL,
    `sponsor_nonce` BIGINT NULL,
    `signed_wire` TEXT NOT NULL,
    `tx_hash` CHAR(66) NULL,
    `actual_fee_wei` VARCHAR(78) NOT NULL DEFAULT '',
    `finalized_height` BIGINT NOT NULL DEFAULT 0,
    `credit_voucher_id` BIGINT NULL,
    `credited_usd` DECIMAL(36,18) NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_base_deposit_quote` (`quote_idx`),
    UNIQUE KEY `uk_base_deposit_hash` (`tx_hash`),
    UNIQUE KEY `uk_base_deposit_nonce` (`sponsor`, `sponsor_nonce`),
    UNIQUE KEY `uk_base_deposit_voucher` (`credit_voucher_id`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;

ALTER TABLE `base_deposit_order`
 ADD CONSTRAINT `fk_base_deposit_quote` FOREIGN KEY (`quote_idx`) REFERENCES `deposit_quote_confirmation` (`quote_idx`),
 ADD CONSTRAINT `fk_base_deposit_voucher` FOREIGN KEY (`credit_voucher_id`) REFERENCES `ledger_voucher` (`id`),
 ADD CONSTRAINT `ck_base_deposit_wire` CHECK ((`tx_hash` IS NULL AND `sponsor_nonce` IS NULL AND `signed_wire`='' AND `state` IN ('accepted','review'))
   OR (`tx_hash` IS NOT NULL AND `sponsor_nonce` IS NOT NULL AND `signed_wire`<>'' AND `state` IN ('signed','review','credited'))),
 ADD CONSTRAINT `ck_base_deposit_credit` CHECK ((`credit_voucher_id` IS NULL AND `credited_usd` IS NULL AND `state`<>'credited')
   OR (`state`='credited' AND `credit_voucher_id` IS NOT NULL AND `credited_usd` IS NOT NULL AND `credited_usd`>0 AND `finalized_height`>0 AND `actual_fee_wei`<>''));
