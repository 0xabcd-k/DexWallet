ALTER TABLE `tron_deposit_order`
 ADD COLUMN `asset_id` VARCHAR(96) NOT NULL DEFAULT 'tron-nile:trx',
 ADD COLUMN `actual_energy` BIGINT NULL,
 ADD CONSTRAINT `ck_tron_deposit_asset` CHECK (`asset_id` IN ('tron-nile:trx','tron-nile:usdt:TYGk3YPGFa7ZLhXfDBd5QzN9Ms9DmPC9tn')),
 ADD CONSTRAINT `ck_tron_deposit_energy` CHECK ((`actual_energy` IS NULL OR `actual_energy`>=0) AND (`state`<>'credited' OR (`asset_id`='tron-nile:trx' AND COALESCE(`actual_energy`,0)=0) OR (`asset_id`<>'tron-nile:trx' AND `actual_energy` IS NOT NULL AND `actual_energy`>0)));
