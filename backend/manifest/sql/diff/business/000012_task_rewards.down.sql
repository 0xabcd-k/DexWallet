-- Refuse to discard issued rewards, progress or shared invitations.
CREATE TABLE `task_reward_rollback_guard` (`history_count` BIGINT NOT NULL CHECK (`history_count` = 0));
INSERT INTO `task_reward_rollback_guard` SELECT (SELECT COUNT(*) FROM `task_completion` WHERE rule_version>0) + (SELECT COUNT(*) FROM `reward_grant`) + (SELECT COUNT(*) FROM `task_invitation`) + (SELECT COUNT(*) FROM `user_referral`) + (SELECT COUNT(*) FROM `user_reward_state`);
DROP TABLE `task_reward_rollback_guard`;
ALTER TABLE `task_completion` DROP FOREIGN KEY `fk_completion_invitation`, DROP CHECK `ck_completion_mode`, DROP CHECK `ck_completion_stage`, DROP CHECK `ck_completion_version`;
DROP TABLE `reward_grant`;
DROP TABLE `user_referral`;
DROP TABLE `task_invitation`;
DROP TABLE `user_reward_state`;
ALTER TABLE `task_completion` DROP COLUMN `mode`, DROP COLUMN `participant_stage`, DROP COLUMN `invitation_id`, DROP COLUMN `identity_revision`, DROP COLUMN `binding_version`, DROP COLUMN `member_status`, DROP COLUMN `task_title`, DROP COLUMN `rule_version`, DROP INDEX `ix_completion_user`, DROP INDEX `ix_completion_user_task`;
