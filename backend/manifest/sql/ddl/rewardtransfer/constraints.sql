ALTER TABLE `reward_transfer`
    ADD CONSTRAINT `fk_reward_transfer_user` FOREIGN KEY (`user_id`) REFERENCES `user` (`id`),
    ADD CONSTRAINT `fk_reward_transfer_submission` FOREIGN KEY (`submission_voucher_id`) REFERENCES `ledger_voucher` (`id`),
    ADD CONSTRAINT `fk_reward_transfer_resolution` FOREIGN KEY (`resolution_voucher_id`) REFERENCES `ledger_voucher` (`id`),
    ADD CONSTRAINT `fk_reward_transfer_reviewer` FOREIGN KEY (`reviewer_id`) REFERENCES `admin_user` (`id`),
    ADD CONSTRAINT `ck_reward_transfer_amount` CHECK (`amount` >= 2 AND `amount` = TRUNCATE(`amount`,2)),
    ADD CONSTRAINT `ck_reward_transfer_status` CHECK (`status` IN (_ascii'pending_review',_ascii'completed',_ascii'rejected')),
    ADD CONSTRAINT `ck_reward_transfer_risk` CHECK (`risk_decision` IN (_ascii'allow',_ascii'review'));
