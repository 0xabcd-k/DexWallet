CREATE TABLE `tron_receive_cursor` (
    `asset_id` VARCHAR(180) NOT NULL,
    `height` BIGINT NOT NULL,
    `block_hash` CHAR(64) NOT NULL,
    PRIMARY KEY (`asset_id`)
) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin;

ALTER TABLE `tron_receive_cursor`
 ADD CONSTRAINT `ck_tron_receive_height` CHECK (`height`>0);
