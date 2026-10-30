ALTER TABLE `event_outbox`
    ADD CONSTRAINT `ck_event_source` CHECK (`business_id` > 0 AND `version` > 0 AND `kind` <> '' AND JSON_VALID(`payload`)),
    ADD CONSTRAINT `ck_event_attempts` CHECK (`attempts` >= 0 AND `generation` >= `attempts`),
    ADD CONSTRAINT `ck_event_state` CHECK (`state` IN ('ready', 'running', 'published', 'dead')),
    ADD CONSTRAINT `ck_event_lease` CHECK ((`state` = 'running') = (`lease_until` IS NOT NULL)),
    ADD CONSTRAINT `ck_event_complete` CHECK ((`state` = 'published') = (`completed_at` IS NOT NULL));

ALTER TABLE `background_task`
    ADD CONSTRAINT `fk_task_event` FOREIGN KEY (`event_id`) REFERENCES `event_outbox` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
    ADD CONSTRAINT `ck_task_handler` CHECK (`handler` <> ''),
    ADD CONSTRAINT `ck_task_attempts` CHECK (`attempts` >= 0 AND `generation` >= `attempts`),
    ADD CONSTRAINT `ck_task_state` CHECK (`state` IN ('ready', 'running', 'succeeded', 'dead', 'blocked')),
    ADD CONSTRAINT `ck_task_lease` CHECK ((`state` = 'running') = (`lease_until` IS NOT NULL)),
    ADD CONSTRAINT `ck_task_complete` CHECK ((`state` = 'succeeded') = (`completed_at` IS NOT NULL));
