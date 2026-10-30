-- Retain business identities and parameter comparisons once funds have moved.
CREATE TABLE `transfer_rollback_guard` (`history_count` BIGINT NOT NULL CHECK (`history_count` = 0));
INSERT INTO `transfer_rollback_guard` SELECT COUNT(*) FROM `transfer_order`;
DROP TABLE `transfer_rollback_guard`;
DROP TABLE `transfer_order`;
