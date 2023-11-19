-- Initial database setup. Run as root@

-- Create the database.
CREATE DATABASE IF NOT EXISTS cleanapp;
USE cleanapp;
SHOW DATABASES;

-- Create the report table.
CREATE TABLE IF NOT EXISTS users (
  id VARCHAR(255),
  avatar VARCHAR(255),
  referral VARCHAR(32),
  ts TIMESTAMP default current_timestamp,
  privacy VARCHAR(255),
  agree_toc VARCHAR(255),
  PRIMARY KEY (id)
);
SHOW TABLES;
DESCRIBE TABLE users;
SHOW COLUMNS FROM users;


-- Create the report table.
CREATE TABLE IF NOT EXISTS reports(
  seq INT NOT NULL AUTO_INCREMENT,
  ts TIMESTAMP default current_timestamp,
  id VARCHAR(255) NOT NULL,
  latitude FLOAT NOT NULL,
  longitude FLOAT NOT NULL,
  x INT,
  y INT,
  image LONGBLOB NOT NULL,
  PRIMARY KEY (seq)
);
SHOW TABLES;
DESCRIBE TABLE reports;
SHOW COLUMNS FROM reports;

CREATE TABLE IF NOT EXISTS referrals(
  refkey CHAR(128) NOT NULL,
  refvalue CHAR(32),
  PRIMARY KEY (refkey)
);
SHOW TABLES;
DESCRIBE TABLE referrals;
SHOW COLUMNS FROM referrals;

CREATE TABLE IF NOT EXISTS users_refcodes(
  referral CHAR(32) NOT NULL,
  id VARCHAR(255) NOT NULL,
  PRIMARY KEY (referral)
);
SHOW TABLES;
DESCRIBE TABLE users_refcodes;
SHOW COLUMNS FROM users_refcodes;

-- Create the user.
-- 1. Remove '%' user
--    if the server and mysql run on the same instance.
-- 2. Replace 'dev_pass' with real password for prod deployments.
CREATE USER IF NOT EXISTS 'server'@'localhost' IDENTIFIED BY 'dev_pass';
CREATE USER IF NOT EXISTS 'server'@'%' IDENTIFIED BY 'dev_pass';
SELECT User, Host FROM mysql.user;

-- Grant rights to the user.
GRANT ALL ON cleanapp.* TO 'server'@'localhost';
GRANT ALL ON cleanapp.* TO 'server'@'%';