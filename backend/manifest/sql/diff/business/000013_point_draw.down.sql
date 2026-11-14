-- Refuse to erase published versions or settled draws.
CREATE TABLE `draw_rollback_guard` (`history_count` BIGINT NOT NULL CHECK (`history_count` = 0));
INSERT INTO `draw_rollback_guard` SELECT (SELECT COUNT(*) FROM `point_draw`) + (SELECT COUNT(*) FROM `draw_rule_version`);
DROP TABLE `draw_rollback_guard`;
DROP TABLE `point_draw`;
DROP TABLE `draw_control`;
DROP TABLE `draw_rule_version`;
