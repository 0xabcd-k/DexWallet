CREATE TABLE `user` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `status` VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'active',
    `nickname` VARCHAR(128) NOT NULL DEFAULT '',
    `avatar_url` VARCHAR(2048) NOT NULL DEFAULT '',
    `auth_version` BIGINT NOT NULL DEFAULT 1,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `email_identity` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `user_id` BIGINT NULL DEFAULT NULL,
    `binding_version` BIGINT NOT NULL DEFAULT 0,
    `bound_at` DATETIME(6) NULL DEFAULT NULL,
    `email_address` VARBINARY(512) NOT NULL,
    `email_local_part` VARBINARY(512) NOT NULL,
    `email_domain` VARCHAR(255) NOT NULL,
    `email_original` VARCHAR(512) NOT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_email_address` (`email_address`),
    UNIQUE KEY `uk_email_owner` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `telegram_identity` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `user_id` BIGINT NULL DEFAULT NULL,
    `binding_version` BIGINT NOT NULL DEFAULT 0,
    `bound_at` DATETIME(6) NULL DEFAULT NULL,
    `telegram_user_id` BIGINT NOT NULL,
    `username` VARCHAR(128) NULL DEFAULT NULL,
    `first_name` VARCHAR(256) NOT NULL,
    `last_name` VARCHAR(256) NULL DEFAULT NULL,
    `photo_url` VARCHAR(2048) NULL DEFAULT NULL,
    `language_code` VARCHAR(64) NULL DEFAULT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_telegram_user_id` (`telegram_user_id`),
    UNIQUE KEY `uk_telegram_owner` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
