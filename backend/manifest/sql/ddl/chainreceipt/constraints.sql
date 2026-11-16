ALTER TABLE `chain_transfer_event`
 ADD CONSTRAINT `fk_chain_event_transaction` FOREIGN KEY (`transaction_id`) REFERENCES `chain_observed_transaction` (`id`),
 ADD CONSTRAINT `ck_chain_event_index` CHECK (`instruction_index` >= 0),
 ADD CONSTRAINT `ck_chain_event_decimals` CHECK (`decimals` BETWEEN 0 AND 18);
