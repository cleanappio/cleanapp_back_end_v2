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
  ts TIMESTAMP DEFAULT current_timestamp,
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
  team INT, -- 0 UNKNOWN, 1 BLUE, 2 GREEN, see map.go
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
  PRIMARY KEY (refkey)
);
SHOW TABLES;
DESCRIBE TABLE referrals;
SHOW COLUMNS FROM referrals;

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