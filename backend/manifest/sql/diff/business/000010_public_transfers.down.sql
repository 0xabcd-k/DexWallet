-- Preserve the policies and identity restrictions used for live payments.
CREATE TABLE public_transfer_rollback_guard (history_count BIGINT NOT NULL CHECK (history_count=0));
INSERT INTO public_transfer_rollback_guard SELECT COUNT(*) FROM transfer_order;
INSERT INTO public_transfer_rollback_guard SELECT COUNT(*) FROM transfer_limit_plan WHERE id<>1;
INSERT INTO public_transfer_rollback_guard SELECT COUNT(*) FROM `user` WHERE identity_revision>1 OR payment_restricted_until IS NOT NULL;
DROP TABLE public_transfer_rollback_guard;
ALTER TABLE transfer_order DROP FOREIGN KEY fk_transfer_plan, DROP INDEX ix_transfer_sender_day, DROP COLUMN limit_plan_id, DROP COLUMN sender_nickname, DROP COLUMN recipient_nickname;
ALTER TABLE `user` DROP FOREIGN KEY fk_user_transfer_plan, DROP COLUMN transfer_limit_plan_id, DROP COLUMN payment_restricted_until, DROP COLUMN identity_revision, DROP COLUMN last_email_identity_id, DROP COLUMN last_telegram_identity_id;
DROP TABLE transfer_limit_plan;
