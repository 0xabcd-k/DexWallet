CREATE TABLE `ton_receive_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `ton_receive_rollback_guard` SELECT COUNT(*) FROM `ton_receive_cursor`;
DROP TABLE `ton_receive_rollback_guard`;
DROP TABLE `ton_receive_candidate`;
DROP TABLE `ton_receive_cursor`;
