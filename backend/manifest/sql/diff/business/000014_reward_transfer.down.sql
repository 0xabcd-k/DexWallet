-- Never erase applications or their fund evidence.
CREATE TABLE `reward_transfer_rollback_guard` (`history_count` BIGINT NOT NULL CHECK (`history_count` = 0));
INSERT INTO `reward_transfer_rollback_guard` SELECT COUNT(*) FROM `reward_transfer`;
DROP TABLE `reward_transfer_rollback_guard`;
DROP TABLE `reward_transfer`;
