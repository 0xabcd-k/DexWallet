ALTER TABLE `chain_observed_transaction`
 ADD `call_trace_fingerprint` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '' AFTER `evidence_json`,
 ADD `call_trace_json` MEDIUMTEXT NULL AFTER `call_trace_fingerprint`;
