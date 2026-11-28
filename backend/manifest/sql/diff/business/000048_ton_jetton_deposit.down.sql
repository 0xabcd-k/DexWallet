CREATE TABLE `ton_jetton_deposit_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `ton_jetton_deposit_rollback_guard` SELECT COUNT(*) FROM `ton_deposit_order` WHERE jetton_payment_key IS NOT NULL;
DROP TABLE `ton_jetton_deposit_rollback_guard`;
ALTER TABLE `ton_deposit_order` DROP CHECK `ck_ton_deposit_jetton_costs`, DROP INDEX `uk_ton_jetton_payment`, DROP COLUMN `jetton_payment_key`, DROP COLUMN `jetton_sender_fee_nano`, DROP COLUMN `jetton_excess_nano`, DROP COLUMN `jetton_reserve_nano`, DROP COLUMN `relay_surplus_nano`;
