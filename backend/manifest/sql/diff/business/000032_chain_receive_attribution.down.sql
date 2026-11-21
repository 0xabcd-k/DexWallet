-- Preserve issued addresses, references and historical assignments.
CREATE TABLE `receive_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used`=0));
INSERT INTO `receive_rollback_guard` SELECT COUNT(*) FROM `chain_receive_address`;
DROP TABLE `receive_rollback_guard`;
DROP TABLE `chain_receive_request`;
DROP TABLE `chain_receive_address`;
