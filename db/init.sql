-- Initial database setup. Run as root@

-- Create the database.
CREATE DATABASE IF NOT EXISTS cleanapp;
USE cleanapp;
SHOW DATABASES;

-- Create the report table.
CREATE TABLE IF NOT EXISTS users (
  id VARCHAR(255),
  avatar VARCHAR(255),
  team INT, -- 0 UNKNOWN, 1 BLUE, 2 GREEN, see map.go
  privacy varchar(255),
  agree_toc varchar(255),
  referral VARCHAR(32),
  kitns_daily INT DEFAULT 0,
  kitns_disbursed INT DEFAULT 0,
  kitns_ref_daily DECIMAL(18, 6) DEFAULT 0.0,
  kitns_ref_disbursed DECIMAL(18, 6) DEFAULT 0.0,
  kitns_ref_redeemed INT DEFAULT 0,
  ts TIMESTAMP default current_timestamp,
  PRIMARY KEY (id),
  UNIQUE INDEX avatar_idx (avatar)
);
SHOW TABLES;
DESCRIBE TABLE users;
SHOW COLUMNS FROM users;

-- Create the report table.
CREATE TABLE IF NOT EXISTS reports(
  seq INT NOT NULL AUTO_INCREMENT,
  ts TIMESTAMP default current_timestamp,
  id VARCHAR(255) NOT NULL,
  team INT NOT NULL, -- 0 UNKNOWN, 1 BLUE, 2 GREEN, see map.go
  latitude FLOAT NOT NULL,
  longitude FLOAT NOT NULL,
  x FLOAT, # 0.0..1.0
  y FLOAT, # 0.0..1.0
  image LONGBLOB NOT NULL,
  PRIMARY KEY (seq)
);
SHOW TABLES;
DESCRIBE TABLE reports;
SHOW COLUMNS FROM reports;

CREATE TABLE IF NOT EXISTS referrals(
  refkey CHAR(128) NOT NULL,
  refvalue CHAR(32),
  PRIMARY KEY (refkey),
  INDEX ref_idx (refvalue)
);
SHOW TABLES;
DESCRIBE TABLE referrals;
SHOW COLUMNS FROM referrals;

CREATE TABLE IF NOT EXISTS users_refcodes(
  referral CHAR(32) NOT NULL,
  id VARCHAR(255) NOT NULL,
  PRIMARY KEY (id)
);
SHOW TABLES;
DESCRIBE TABLE users_refcodes;
SHOW COLUMNS FROM users_refcodes;

-- Create the user.
-- 1. Remove '%' user
--    if the server and mysql run on the same instance.
--    (still needed if run from two images)
CREATE USER IF NOT EXISTS 'server'@'localhost' IDENTIFIED BY 'secret_app';
CREATE USER IF NOT EXISTS 'server'@'%' IDENTIFIED BY 'secret_app';
CREATE USER IF NOT EXISTS 'importer'@'%' IDENTIFIED BY 'secret_importer';
SELECT User, Host FROM mysql.user;

-- Grant rights to the user.
GRANT ALL ON cleanapp.* TO 'server'@'localhost';
GRANT ALL ON cleanapp.* TO 'server'@'%';
GRANT SELECT ON cleanapp.* TO 'importer'@'%';  -- We don't make secret out of reports, so that's safe.
