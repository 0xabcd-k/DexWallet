CREATE TABLE `sol_receive_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `sol_receive_rollback_guard` SELECT COUNT(*) FROM `solana_receive_cursor`;
DROP TABLE `sol_receive_rollback_guard`;
DROP TABLE `solana_receive_candidate`;
DROP TABLE `solana_receive_cursor`;
