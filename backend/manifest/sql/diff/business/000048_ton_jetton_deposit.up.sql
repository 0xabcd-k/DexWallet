ALTER TABLE `ton_deposit_order`
 ADD COLUMN `jetton_payment_key` CHAR(64) NULL,
 ADD COLUMN `jetton_sender_fee_nano` BIGINT NULL,
 ADD COLUMN `jetton_excess_nano` BIGINT NULL,
 ADD COLUMN `jetton_reserve_nano` BIGINT NULL,
 ADD COLUMN `relay_surplus_nano` BIGINT NULL,
 ADD UNIQUE KEY `uk_ton_jetton_payment` (`jetton_payment_key`),
 ADD CONSTRAINT `ck_ton_deposit_jetton_costs` CHECK ((jetton_sender_fee_nano IS NULL AND jetton_excess_nano IS NULL AND jetton_reserve_nano IS NULL AND relay_surplus_nano IS NULL AND (jetton_payment_key IS NULL OR state <> 'credited')) OR (jetton_payment_key IS NOT NULL AND receive_hash IS NOT NULL AND actual_fee_nano IS NOT NULL AND jetton_sender_fee_nano IS NOT NULL AND jetton_sender_fee_nano BETWEEN 0 AND actual_fee_nano AND jetton_excess_nano IS NOT NULL AND jetton_excess_nano>0 AND jetton_reserve_nano IS NOT NULL AND jetton_reserve_nano>=0 AND relay_surplus_nano IS NOT NULL AND relay_surplus_nano>=0));
