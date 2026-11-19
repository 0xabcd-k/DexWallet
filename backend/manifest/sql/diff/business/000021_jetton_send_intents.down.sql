-- Keep Jetton authorizations and their immutable execution budgets.
CREATE TABLE `jetton_send_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used` = 0));
INSERT INTO `jetton_send_rollback_guard` SELECT COUNT(*) FROM `chain_send_intent` WHERE `jetton_json` <> '';
DROP TABLE `jetton_send_rollback_guard`;
ALTER TABLE `chain_send_intent` DROP CHECK `ck_chain_send_jetton_json`, DROP COLUMN `jetton_json`;
