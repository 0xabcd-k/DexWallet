CREATE TABLE `tron_deposit_order` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `quote_idx` CHAR(32) NOT NULL,
    `tx_id` CHAR(64) NOT NULL,
    `user_signature` CHAR(130) NOT NULL,
    `order_signature` CHAR(130) NOT NULL,
    `accepted_at` DATETIME(3) NOT NULL,
    `state` VARCHAR(16) NOT NULL,
    `actual_fee_sun` BIGINT NULL,
    `actual_bandwidth` BIGINT NULL,
    `finalized_height` BIGINT NULL,
    `credit_voucher_id` BIGINT NULL,
    `credited_usd` DECIMAL(36,18) NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_tron_deposit_quote` (`quote_idx`),
    UNIQUE KEY `uk_tron_deposit_tx` (`tx_id`),
    UNIQUE KEY `uk_tron_deposit_voucher` (`credit_voucher_id`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;

ALTER TABLE `tron_deposit_order`
 ADD CONSTRAINT `fk_tron_deposit_quote` FOREIGN KEY (`quote_idx`) REFERENCES `deposit_quote_confirmation` (`quote_idx`),
 ADD CONSTRAINT `fk_tron_deposit_voucher` FOREIGN KEY (`credit_voucher_id`) REFERENCES `ledger_voucher` (`id`),
 ADD CONSTRAINT `ck_tron_deposit_state` CHECK (`state` IN ('signed','review','credited')),
 ADD CONSTRAINT `ck_tron_deposit_credit` CHECK ((`state`='credited' AND `credit_voucher_id` IS NOT NULL AND `credited_usd` IS NOT NULL AND `credited_usd`>0 AND `actual_fee_sun` IS NOT NULL AND `actual_fee_sun`=0 AND `actual_bandwidth` IS NOT NULL AND `actual_bandwidth`>0 AND `finalized_height` IS NOT NULL AND `finalized_height`>0)
    OR (`state`<>'credited' AND `credit_voucher_id` IS NULL AND `credited_usd` IS NULL));
