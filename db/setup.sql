# Initial database setup. Run as root@

# Create the database.
create database cleanapp;
use cleanapp;
show databases;

# Create the report table.
CREATE TABLE users (
  id varchar(255),
  avatar varchar(255),
  ts timestamp default current_timestamp,
  PRIMARY KEY (id)
);
show tables;
describe table users;
show columns from users;


# Create the report table.
CREATE TABLE reports(
  seq INT NOT NULL AUTO_INCREMENT,
  ts timestamp default current_timestamp,
  id varchar(255) NOT NULL,
  latitude float NOT NULL,
  longitude float NOT NULL,
  x int,
  y int,
  image longblob NOT NULL,
  PRIMARY KEY (seq)
);
show tables;
describe table reports;
show columns from reports;

# Create the user.
# 1. Remove '%' user
#    if the server and mysql run on the same instance.
# 2. Replace 'dev_pass' with real password for prod deployments.
CREATE USER 'server'@'localhost' IDENTIFIED BY 'dev_pass';
CREATE USER 'server'@'%' IDENTIFIED BY 'dev_pass';

# Grant rights to the user.
GRANT ALL ON cleanapp.* TO 'server'@'localhost';
GRANT ALL ON cleanapp.* TO 'server'@'%';