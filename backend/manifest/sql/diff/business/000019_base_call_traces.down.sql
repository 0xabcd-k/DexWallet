-- Preserve the evidence required to recover internal ETH events.
CREATE TABLE `base_call_trace_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used` = 0));
INSERT INTO `base_call_trace_rollback_guard`
 SELECT (SELECT COUNT(*) FROM `chain_observed_transaction` WHERE `call_trace_fingerprint` <> '' OR `call_trace_json` IS NOT NULL)
      + (SELECT COUNT(*) FROM `chain_transfer_event` WHERE `event_kind` = 'evm_call');
DROP TABLE `base_call_trace_rollback_guard`;
ALTER TABLE `chain_observed_transaction` DROP `call_trace_fingerprint`, DROP `call_trace_json`;
