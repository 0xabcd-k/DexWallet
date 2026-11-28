CREATE TABLE `base_eth_deposit_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `base_eth_deposit_rollback_guard` SELECT COUNT(*) FROM `base_deposit_order` WHERE `asset_id`='base-sepolia:eth';
DROP TABLE `base_eth_deposit_rollback_guard`;
ALTER TABLE `base_deposit_order` DROP CHECK `ck_base_deposit_asset`, DROP COLUMN `asset_id`;
