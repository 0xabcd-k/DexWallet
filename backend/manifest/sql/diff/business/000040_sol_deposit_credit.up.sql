ALTER TABLE `sol_deposit_order`
 ADD COLUMN `id` BIGINT NOT NULL AUTO_INCREMENT FIRST,
 DROP PRIMARY KEY, ADD PRIMARY KEY (`id`),
 ADD UNIQUE KEY `uk_sol_deposit_quote` (`quote_idx`),
 ADD COLUMN `credit_voucher_id` BIGINT NULL,
 ADD COLUMN `credited_usd` DECIMAL(36,18) NULL,
 ADD COLUMN `credited_at` DATETIME(3) NULL,
 ADD UNIQUE KEY `uk_sol_credit_voucher` (`credit_voucher_id`),
 ADD CONSTRAINT `fk_sol_credit_voucher` FOREIGN KEY (`credit_voucher_id`) REFERENCES `ledger_voucher` (`id`),
 ADD CONSTRAINT `ck_sol_deposit_credit` CHECK (
   (`credit_voucher_id` IS NULL AND `credited_usd` IS NULL AND `credited_at` IS NULL)
   OR (`execution_state`='finalized' AND `credit_voucher_id` IS NOT NULL AND `credited_usd` IS NOT NULL
       AND `credited_usd` > 0 AND `credited_usd` = TRUNCATE(`credited_usd`,2) AND `credited_at` IS NOT NULL)
 );
