-- Preserve idempotency and configuration history after use.
CREATE TABLE `reward_operations_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used` = 0));
INSERT INTO `reward_operations_rollback_guard` SELECT
 (SELECT COUNT(*) FROM `task_definition` WHERE `create_key` IS NOT NULL) +
 (SELECT COUNT(*) FROM `draw_control` WHERE `revision` > 1);
DROP TABLE `reward_operations_rollback_guard`;
ALTER TABLE `draw_control` DROP COLUMN `revision`;
ALTER TABLE `task_definition` DROP INDEX `uk_task_create`, DROP COLUMN `create_hash`, DROP COLUMN `create_key`;
