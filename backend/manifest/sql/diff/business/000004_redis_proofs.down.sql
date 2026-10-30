-- Temporary authentication state is recreated empty on rollback.
CREATE TABLE `email_challenge` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `email_address` VARBINARY(512) NOT NULL,
    `challenge_token` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    `code_digest` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    `purpose` VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    `actor_id` BIGINT NOT NULL DEFAULT 0,
    `actor_version` BIGINT NOT NULL DEFAULT 0,
    `status` VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'pending',
    `attempts` BIGINT NOT NULL DEFAULT 0,
    `issued_at` DATETIME(6) NOT NULL DEFAULT '1970-01-01 00:00:00',
    `expires_at` DATETIME(6) NOT NULL DEFAULT '1970-01-01 00:00:00',
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_challenge_email` (`email_address`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `auth_operation` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `request_digest` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `input_digest` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `proof_digest` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL DEFAULT NULL,
    `purpose` VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `identity_type` VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `actor_id` BIGINT NOT NULL DEFAULT 0,
    `actor_version` BIGINT NOT NULL DEFAULT 0,
    `user_id` BIGINT NOT NULL DEFAULT 0,
    `identity_id` BIGINT NOT NULL DEFAULT 0,
    `binding_version` BIGINT NOT NULL DEFAULT 0,
    `auth_version` BIGINT NOT NULL DEFAULT 0,
    `completed` BOOLEAN NOT NULL DEFAULT FALSE,
    `expires_at` DATETIME(6) NOT NULL,
    `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_auth_request` (`request_digest`),
    UNIQUE KEY `uk_auth_proof` (`proof_digest`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE `backoffice_challenge` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `realm` varchar(16) NOT NULL,
  `email` varchar(254) NOT NULL,
  `challenge_token` char(64) NOT NULL DEFAULT '',
  `code_digest` char(64) NOT NULL DEFAULT '',
  `auth_version` bigint NOT NULL DEFAULT 0,
  `status` varchar(16) NOT NULL DEFAULT 'pending',
  `attempts` bigint NOT NULL DEFAULT 0,
  `issued_at` datetime(6) NOT NULL,
  `expires_at` datetime(6) NOT NULL,
  PRIMARY KEY (`id`), UNIQUE KEY `uk_realm_email` (`realm`,`email`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
