-- 仅用于工程隔离测试库；精度不代表任何业务账户约定。
CREATE TABLE contract_sample (
    id BIGINT NOT NULL AUTO_INCREMENT,
    sample_key VARCHAR(64) NOT NULL,
    amount DECIMAL(36,18) NOT NULL,
    optional_amount DECIMAL(36,18) NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    note VARCHAR(128) NOT NULL DEFAULT '',
    PRIMARY KEY (id),
    UNIQUE KEY uk_contract_sample_key (sample_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
