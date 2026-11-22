CREATE TABLE `tron_receive_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `tron_receive_rollback_guard` SELECT COUNT(*) FROM `tron_receive_cursor`;
DROP TABLE `tron_receive_rollback_guard`;
DROP TABLE `tron_receive_cursor`;
