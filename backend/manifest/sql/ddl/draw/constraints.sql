ALTER TABLE `draw_control`
    ADD CONSTRAINT `fk_draw_control_version` FOREIGN KEY (`version_id`) REFERENCES `draw_rule_version` (`id`),
    ADD CONSTRAINT `ck_draw_control_id` CHECK (`id` = 1);
ALTER TABLE `point_draw`
    ADD CONSTRAINT `fk_draw_user` FOREIGN KEY (`user_id`) REFERENCES `user` (`id`),
    ADD CONSTRAINT `fk_draw_version` FOREIGN KEY (`version_id`) REFERENCES `draw_rule_version` (`id`),
    ADD CONSTRAINT `fk_draw_voucher` FOREIGN KEY (`voucher_id`) REFERENCES `ledger_voucher` (`id`),
    ADD CONSTRAINT `ck_draw_cost` CHECK (`cost_points` = 100),
    ADD CONSTRAINT `ck_draw_reward` CHECK (`reward_usd` > 0 AND `reward_usd` = TRUNCATE(`reward_usd`,2)),
    ADD CONSTRAINT `ck_draw_ticket` CHECK (`ticket` >= 0 AND `ticket` < 10000);
INSERT INTO `draw_control` (`id`) VALUES (1);
