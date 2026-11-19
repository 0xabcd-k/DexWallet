ALTER TABLE `chain_send_intent`
 ADD COLUMN `jetton_json` VARCHAR(1024) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '' AFTER `amount_atoms`,
 ADD CONSTRAINT `ck_chain_send_jetton_json` CHECK (`jetton_json` = '' OR JSON_VALID(`jetton_json`));
