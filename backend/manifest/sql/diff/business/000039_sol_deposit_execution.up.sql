ALTER TABLE `sol_deposit_order`
 ADD COLUMN `execution_state` VARCHAR(16) NOT NULL DEFAULT 'waiting',
 ADD COLUMN `sponsor_policy` VARCHAR(48) NOT NULL DEFAULT '',
 ADD COLUMN `tx_signature` VARCHAR(88) NULL,
 ADD COLUMN `actual_lamports_fee` BIGINT NOT NULL DEFAULT 0,
 ADD COLUMN `finalized_slot` BIGINT NOT NULL DEFAULT 0,
 ADD UNIQUE KEY `uk_sol_deposit_tx` (`tx_signature`),
 ADD CONSTRAINT `ck_sol_deposit_execution` CHECK (
   (`execution_state` = 'waiting' AND `tx_signature` IS NULL OR
    `execution_state` = 'review' AND `accepted_at` IS NOT NULL OR
    `execution_state` IN ('signed','submitted','finalized','failed') AND `accepted_at` IS NOT NULL AND `tx_signature` IS NOT NULL)
   AND ((`tx_signature` IS NULL AND `sponsor_policy` = '') OR (`tx_signature` IS NOT NULL AND `sponsor_policy` <> ''))
   AND ((`execution_state` IN ('finalized','failed') AND `finalized_slot` > 0 AND `actual_lamports_fee` > 0)
     OR (`execution_state` NOT IN ('finalized','failed') AND `finalized_slot` = 0 AND `actual_lamports_fee` = 0))
 );
