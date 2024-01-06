# Cleanapp Backend version 2+

This repository is for CleanApp (http://cleanapp.io) backend development.
It's a complete rewrite after v.0.

## Installation

Pre-requisites: Linux (Debian/Ubuntu/...), this is tested on Google Drive Ubuntu VPS instance.

1. Login to the target machine.
2. Get setup.sh into the current directory, e.g. using
```shell
curl https://raw.githubusercontent.com/cleanappio/cleanapp_back_end_v2/main/setup/setup.sh > setup.sh
```
3. Run
```
./setup.sh
```
4. When doing it, you will be asked to set the DB passwords:

* MYSQL_ROOT_PASSWORD for MySQL root user password.
* MYSQL_APP_PASSWORD for MySQL password for the API server.
* MYSQL_READER_PASSWORD for MySQL password for database reading/import.

It should be up and running now. If not, contact eldarm@cleanapp.io

## Operations

1. Stopping:
```
./down.sh
```
2. Restarting after a stop:
```
./up.sh
```
3. Stopping with deletion of the database:
```
sudo docker-compose down -v
```

## Direct dependencies

Docker images (1.6 is 2.0(Alpha) version):
1. BE API server: ibnazer/cleanappserver:1.6
2. BE Database: ibnazer/cleanappdb:1.6
3. BE application server (currently referral redirection service only): ibnazer/cleanappapp:1.6

## More

More infro is to be added.

