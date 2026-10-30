-- Event/task identities must survive retries, restarts and delayed MQ messages.
CREATE TABLE `background_rollback_guard` (`history_count` BIGINT NOT NULL CHECK (`history_count` = 0));
INSERT INTO `background_rollback_guard` SELECT (SELECT COUNT(*) FROM `event_outbox`) + (SELECT COUNT(*) FROM `background_task`);
DROP TABLE `background_rollback_guard`;
DROP TABLE `background_task`;
DROP TABLE `event_outbox`;
