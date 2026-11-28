CREATE TABLE `tron_usdt_deposit_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `tron_usdt_deposit_rollback_guard` SELECT COUNT(*) FROM `tron_deposit_order` WHERE `asset_id`<>'tron-nile:trx';
DROP TABLE `tron_usdt_deposit_rollback_guard`;
ALTER TABLE `tron_deposit_order` DROP CHECK `ck_tron_deposit_asset`, DROP CHECK `ck_tron_deposit_energy`, DROP COLUMN `asset_id`, DROP COLUMN `actual_energy`;
