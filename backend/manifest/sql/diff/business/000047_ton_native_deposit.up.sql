CREATE TABLE `ton_deposit_order` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `quote_idx` CHAR(32) NOT NULL,
    `auth_hash` CHAR(64) NOT NULL,
    `user_signature` CHAR(128) NOT NULL,
    `order_signature` CHAR(128) NOT NULL,
    `accepted_at` DATETIME(3) NOT NULL,
    `state` VARCHAR(16) NOT NULL,
    `platform_signature` CHAR(128) NULL,
    `dispatch_hash` CHAR(64) NULL,
    `receive_hash` CHAR(64) NULL,
    `execution_failed` TINYINT NOT NULL DEFAULT 0,
    `actual_fee_nano` BIGINT NULL,
    `credit_voucher_id` BIGINT NULL,
    `credited_usd` DECIMAL(36,18) NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_ton_deposit_quote` (`quote_idx`),
    UNIQUE KEY `uk_ton_deposit_auth` (`auth_hash`),
    UNIQUE KEY `uk_ton_deposit_dispatch` (`dispatch_hash`),
    UNIQUE KEY `uk_ton_deposit_receive` (`receive_hash`),
    UNIQUE KEY `uk_ton_deposit_voucher` (`credit_voucher_id`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;

ALTER TABLE `ton_deposit_order`
 ADD CONSTRAINT `fk_ton_deposit_quote` FOREIGN KEY (`quote_idx`) REFERENCES `deposit_quote_confirmation` (`quote_idx`),
 ADD CONSTRAINT `fk_ton_deposit_voucher` FOREIGN KEY (`credit_voucher_id`) REFERENCES `ledger_voucher` (`id`),
 ADD CONSTRAINT `ck_ton_deposit_state` CHECK (`state` IN ('accepted','signed','credited','review')),
 ADD CONSTRAINT `ck_ton_deposit_failure` CHECK (execution_failed=0 OR (execution_failed=1 AND state='review' AND dispatch_hash IS NOT NULL)),
 ADD CONSTRAINT `ck_ton_deposit_dispatch` CHECK ((`platform_signature` IS NULL AND `dispatch_hash` IS NULL AND `state` IN ('accepted','review')) OR (`platform_signature` IS NOT NULL AND `dispatch_hash` IS NOT NULL AND `state` IN ('signed','review','credited'))),
 ADD CONSTRAINT `ck_ton_deposit_credit` CHECK ((`state`='credited' AND `receive_hash` IS NOT NULL AND `actual_fee_nano` IS NOT NULL AND `actual_fee_nano`>0 AND `credit_voucher_id` IS NOT NULL AND `credited_usd` IS NOT NULL AND `credited_usd`>0) OR (`state`<>'credited' AND `credit_voucher_id` IS NULL AND `credited_usd` IS NULL));
