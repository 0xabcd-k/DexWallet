CREATE TABLE `base_receive_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `base_receive_rollback_guard` SELECT COUNT(*) FROM `base_receive_cursor`;
DROP TABLE `base_receive_rollback_guard`;
DROP TABLE `base_receive_candidate`;
DROP TABLE `base_receive_cursor`;
ALTER TABLE `chain_receive_request` DROP KEY `idx_receive_asset_open`;
