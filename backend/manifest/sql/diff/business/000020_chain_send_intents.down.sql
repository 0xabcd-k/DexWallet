-- Signed or even unsigned intents reserve a business identity and wallet seqno.
CREATE TABLE `chain_send_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used` = 0));
INSERT INTO `chain_send_rollback_guard` SELECT COUNT(*) FROM `chain_send_intent`;
DROP TABLE `chain_send_rollback_guard`;
DROP TABLE `chain_send_intent`;
