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

## Open ports

* API server exposes port 8080.
* APP server exposes port 3000.
* MySQL DB uses port 3306 but currently does not expose it externally. Do so,
if you want to connect to it from outside.

### How to expose port in Google Cloud

Caveat: Google Cloud UI is not stable, so the instruction below may become obsolete. This is the status on January 2024.

On the account level you need to create firewall rules "allow-8080" and "allow-3000"

Dashboard -> VPC Network -> Firewall, look at VPC Firewall Rules.

It will have the list of available rules.
On top of the page (!Not near the table!) there will be a button "Create Firewall Rule"

- Name: allow-8080
- Description: Allow port 8080.
- Target tags: allow-8080
- Source filters, IP ranges: 0.0.0.0/0
- Protocols and ports: tcp:8080
- It's ok to leaave the rest default.

Create another rule for port 3000.

You are almost done. Now in Compute Engine > VM Instances select the one you want to use. Pick Edit at the top. Go to network tags and add "allow-8080" and "allow-3000". Save. 

You are ready to deploy on this VM.

## Verifying once set up

From outside try:
- http://dev.api.cleanapp.io:8080/help
- http://dev.app.cleanapp.io:3000/help

Both times you will get a plain short welcome message with CleanApp API/APP version. 

## More

More infro is to be added.

