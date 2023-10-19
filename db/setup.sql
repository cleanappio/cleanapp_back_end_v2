# Initial database setup. Run as root@

# Create the database.

CREATE DATABASE cleanapp;
SHOW databases;
USE cleanapp;

# Create the table.

CREATE TABLE reports(
    seq INT NOT NULL AUTO_INCREMENT,
    ts timestamp DEFAULT CURRENT_TIMESTAMP,
    id VARCHAR(255),
    lattitude FLOAT,
    longitude FLOAT,
    x INT,
    y INT,
    image LONGBLOB,
    PRIMARY KEY (seq)
);
SHOW TABLES;
DESCRIBE TABLE reports;
SHOW columns FROM reports;

# Create the user.
# 1. Remove '%' user
#    if the server and mysql run on the same instance.
# 2. Replace 'dev_pass' with real password for prod deployments.

CREATE USER 'server'@'localhost' IDENTIFIED BY 'dev_pass';
CREATE USER 'server'@'%' IDENTIFIED BY 'dev_pass';

# Grant rights to the user.

GRANT ALL ON cleanapp.* TO 'server'@'localhost';
GRANT ALL ON cleanapp.* TO 'server'@'%';
