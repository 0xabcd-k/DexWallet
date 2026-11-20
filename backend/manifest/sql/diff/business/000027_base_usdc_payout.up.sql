ALTER TABLE `base_payout`
 ADD COLUMN `asset_id` VARCHAR(64) NOT NULL DEFAULT 'base-sepolia:eth' AFTER `wei_amount`,
 ADD COLUMN `token_atoms` VARCHAR(78) NOT NULL DEFAULT '' AFTER `asset_id`,
 ADD COLUMN `transfer_log_index` BIGINT NULL AFTER `token_atoms`;
ALTER TABLE `base_payout`
 ADD CONSTRAINT `ck_base_payout_asset` CHECK (
    (`asset_id`='base-sepolia:eth' AND `token_atoms`='' AND `transfer_log_index` IS NULL)
    OR (`asset_id`='base-sepolia:usdc' AND `wei_amount`='0' AND CHAR_LENGTH(`token_atoms`)>0
        AND ((`state`='confirmed' AND `transfer_log_index` IS NOT NULL AND `transfer_log_index` BETWEEN 0 AND 2147483647)
             OR (`state`<>'confirmed' AND `transfer_log_index` IS NULL))));
