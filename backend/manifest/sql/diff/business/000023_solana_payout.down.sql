CREATE TABLE `solana_payout_rollback_guard` (`used` BIGINT NOT NULL CHECK (`used` = 0));
INSERT INTO `solana_payout_rollback_guard` SELECT COUNT(*) FROM `solana_payout`;
DROP TABLE `solana_payout_rollback_guard`;
DROP TABLE `solana_payout`;
