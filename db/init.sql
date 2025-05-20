-- Initial database setup. Run as root@

-- Create the database.
CREATE DATABASE IF NOT EXISTS cleanapp;
USE cleanapp;
SHOW DATABASES;

-- Create the users table.
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
  action_id VARCHAR(32),
  ts TIMESTAMP default current_timestamp,
  PRIMARY KEY (id),
  UNIQUE INDEX avatar_idx (avatar),
  INDEX action_idx (action_id)
);
SHOW TABLES;
DESCRIBE TABLE users;
SHOW COLUMNS FROM users;

-- Create the users shadow table.
CREATE TABLE IF NOT EXISTS users_shadow (
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
  action_id VARCHAR(32),
  ts TIMESTAMP default current_timestamp,
  PRIMARY KEY (id),
  UNIQUE INDEX avatar_idx (avatar),
  INDEX action_idx (action_id)
);
SHOW TABLES;
DESCRIBE TABLE users_shadow;
SHOW COLUMNS FROM users_shadow;

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
  action_id VARCHAR(32),
  PRIMARY KEY (seq),
  INDEX id_index (id),
  INDEX action_idx (action_id)
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

CREATE TABLE IF NOT EXISTS actions(
  id CHAR(32) NOT NULL,
  name VARCHAR(255) NOT NULL,
  is_active INT NOT NULL DEFAULT 0,
  expiration_date DATE,
  PRIMARY KEY (id)
);
SHOW TABLES;
DESCRIBE TABLE actions;
SHOW COLUMNS FROM actions;

CREATE TABLE IF NOT EXISTS areas(
  id INT NOT NULL AUTO_INCREMENT,
  name VARCHAR(255) NOT NULL,
  description VARCHAR(255),
  is_custom BOOL NOT NULL DEFAULT false,
  contact_name VARCHAR(255),
  area_json JSON,
  created_at TIMESTAMP,
  updated_at TIMESTAMP,
  PRIMARY KEY (id)
 );

CREATE TABLE IF NOT EXISTS contact_emails(
  area_id INT NOT NULL,
  email CHAR(64) NOT NULL,
  consent_report BOOL NOT NULL DEFAULT true,
  INDEX area_id_index (area_id),
  INDEX email_index (email)
);

CREATE TABLE IF NOT EXISTS area_index(
  area_id INT NOT NULL,
  geom GEOMETRY NOT NULL SRID 4326,
  SPATIAL INDEX(geom)
);

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
