CREATE TABLE `tron_usdt_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `tron_usdt_rollback_guard` SELECT COUNT(*) FROM `tron_payout` WHERE `asset_id`<>'tron-nile:trx';
DROP TABLE `tron_usdt_rollback_guard`;
ALTER TABLE `tron_payout`
 DROP CHECK `ck_tron_payout_source`, DROP CHECK `ck_tron_payout_asset`, DROP COLUMN `asset_id`, DROP COLUMN `token_contract`, DROP COLUMN `token_atoms`, DROP COLUMN `fee_limit_sun`, DROP COLUMN `transfer_log_index`;
ALTER TABLE `tron_payout`
 ADD CONSTRAINT `ck_tron_payout_source` CHECK (`network`='tron-nile' AND `sun`>0 AND `ref_height`>0 AND `expiration_ms`>`timestamp_ms` AND `timestamp_ms`>0 AND `attempts`>=0);
