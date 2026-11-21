ALTER TABLE `tron_payout`
 ADD COLUMN `asset_id` VARCHAR(128) NOT NULL DEFAULT 'tron-nile:trx' AFTER `sun`,
 ADD COLUMN `token_contract` VARCHAR(34) NOT NULL DEFAULT '' AFTER `asset_id`,
 ADD COLUMN `token_atoms` VARCHAR(78) NOT NULL DEFAULT '' AFTER `token_contract`,
 ADD COLUMN `fee_limit_sun` BIGINT NOT NULL DEFAULT 0 AFTER `token_atoms`,
 ADD COLUMN `transfer_log_index` BIGINT NULL AFTER `fee_limit_sun`;
ALTER TABLE `tron_payout`
 DROP CHECK `ck_tron_payout_source`;
ALTER TABLE `tron_payout`
 ADD CONSTRAINT `ck_tron_payout_source` CHECK (`network`='tron-nile' AND `ref_height`>0 AND `expiration_ms`>`timestamp_ms` AND `timestamp_ms`>0 AND `attempts`>=0),
 ADD CONSTRAINT `ck_tron_payout_asset` CHECK (
    (`asset_id`='tron-nile:trx' AND `sun`>0 AND `token_contract`='' AND `token_atoms`='' AND `fee_limit_sun`=0 AND `transfer_log_index` IS NULL)
    OR (`sun`=0 AND CHAR_LENGTH(`token_contract`)=34 AND `asset_id`=CONCAT('tron-nile:usdt:',`token_contract`) AND CHAR_LENGTH(`token_atoms`)>0 AND `fee_limit_sun`>0
        AND ((`state`='confirmed' AND `transfer_log_index` IS NOT NULL AND `transfer_log_index` BETWEEN 0 AND 2147483647)
            OR (`state`<>'confirmed' AND `transfer_log_index` IS NULL))));
