ALTER TABLE `base_deposit_order`
 ADD COLUMN `asset_id` VARCHAR(32) NOT NULL DEFAULT 'base-sepolia:usdc' AFTER `quote_idx`,
 ADD CONSTRAINT `ck_base_deposit_asset` CHECK (`asset_id` IN ('base-sepolia:usdc','base-sepolia:eth'));
