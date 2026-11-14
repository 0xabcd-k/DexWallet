-- Never discard already consumed eligibility or business configuration.
CREATE TABLE `task_rollback_guard` (`history_count` BIGINT NOT NULL CHECK (`history_count` = 0));
INSERT INTO `task_rollback_guard` SELECT (SELECT COUNT(*) FROM `task_definition`) + (SELECT COUNT(*) FROM `task_completion`);
DROP TABLE `task_rollback_guard`;
DROP TABLE `task_completion`;
DROP TABLE `task_definition`;
