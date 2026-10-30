CREATE TABLE `admin_user` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `email` varchar(254) COLLATE utf8mb4_bin NOT NULL,
  `password_hash` varchar(100) NOT NULL,
  `status` varchar(16) NOT NULL DEFAULT 'active',
  `auth_version` bigint NOT NULL DEFAULT 1,
  `must_change_password` tinyint NOT NULL DEFAULT 1,
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`), UNIQUE KEY `uk_email` (`email`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `merchant_user` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `email` varchar(254) COLLATE utf8mb4_bin NOT NULL,
  `password_hash` varchar(100) NOT NULL,
  `status` varchar(16) NOT NULL DEFAULT 'active',
  `auth_version` bigint NOT NULL DEFAULT 1,
  `must_change_password` tinyint NOT NULL DEFAULT 1,
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`), UNIQUE KEY `uk_email` (`email`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `backoffice_audit` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `actor_realm` varchar(16) NOT NULL,
  `actor_id` bigint NOT NULL DEFAULT 0,
  `action` varchar(32) NOT NULL,
  `target_realm` varchar(16) NOT NULL,
  `target_id` bigint NOT NULL DEFAULT 0,
  `target_email` varchar(254) NOT NULL DEFAULT '',
  `before_value` varchar(512) NOT NULL DEFAULT '',
  `after_value` varchar(512) NOT NULL DEFAULT '',
  `reason` varchar(512) NOT NULL DEFAULT '',
  `result` varchar(16) NOT NULL,
  `trace_id` varchar(64) NOT NULL DEFAULT '',
  `created_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`), KEY `idx_created` (`created_at`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
