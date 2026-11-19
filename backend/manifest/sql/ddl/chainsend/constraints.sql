ALTER TABLE `chain_send_intent`
 ADD CONSTRAINT `ck_chain_send_jetton_json` CHECK (`jetton_json` = '' OR JSON_VALID(`jetton_json`)),
 ADD CONSTRAINT `ck_chain_send_seqno` CHECK (`seqno` BETWEEN 1 AND 4294967295),
 ADD CONSTRAINT `ck_chain_send_subwallet` CHECK (`subwallet_id` BETWEEN 0 AND 4294967295),
 ADD CONSTRAINT `ck_chain_send_expiry` CHECK (`valid_until` BETWEEN 1 AND 4294967295),
 ADD CONSTRAINT `ck_chain_send_attempts` CHECK (`attempts` >= 0),
 ADD CONSTRAINT `ck_chain_send_state` CHECK (`state` IN ('prepared','signed','broadcast_unknown','submitted')),
 ADD CONSTRAINT `ck_chain_send_artifact` CHECK ((`state`='prepared' AND `signed_boc` IS NULL AND `message_hash`='' AND `body_hash`='') OR (`state`<>'prepared' AND `signed_boc` IS NOT NULL AND CHAR_LENGTH(`message_hash`)=64 AND CHAR_LENGTH(`body_hash`)=64));
