ALTER TABLE `task_definition`
 ADD COLUMN `create_key` VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER `revision`,
 ADD COLUMN `create_hash` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER `create_key`,
 ADD UNIQUE KEY `uk_task_create` (`create_key`);
ALTER TABLE `draw_control` ADD COLUMN `revision` BIGINT NOT NULL DEFAULT 1 AFTER `version_id`;
