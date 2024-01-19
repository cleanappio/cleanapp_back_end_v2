# Cleanapp Backend version 2+

This repository is for CleanApp (http://cleanapp.io) backend development.

# Installation

## Pre-requisites

1.  Make sure that your local machine has Docker installed. https://docs.docker.com/engine/install/
1.  Make sure you're prepared for working with GoogleCloud.
    1.  You got necessary access to Google Cloud services. Ask project admins for them.
    1.  You have gcloud command line interface installed, https://cloud.google.com/sdk/docs/install
    1.  You are successfully logged in gcloud, https://cloud.google.com/sdk/gcloud/reference/auth/login

## Build Docker image

1.  Modify the Docker image version if necessary. Open the file `docker/.version` and set the desired value of the `BUILD_VERSION`.
1.  Run the `docker/build_server_image.sh` from the `docker` directory.
    ```
    cd docker &&
    ./build_server_image.sh
    ```

## Deploying in Google Cloud

Pre-requisites: Linux (Debian/Ubuntu/...), this is tested on Google Cloud Ubuntu VPS instance.

1. Login to the target machine.
   * On GCloud you go to the dashboard, pick the instance, and the click on SSH
1. Get setup.sh into the current directory, e.g. using
```shell
curl https://raw.githubusercontent.com/cleanappio/cleanapp_back_end_v2/main/setup/setup.sh > setup.sh &&
sudo chmod a+x setup.h
```
1. Run
```
./setup.sh
```

It should be up and running now.

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
4. Refreshing images to the newly built versions:
    1. Stop services
    2. Delete loaded images (```docker images``` and ```docker image``` commands, you may need to use -f flag)
    3. If you need a different label or prefix, edit ```docker-compose.yaml``` file.
    4. (preferable) Load new images using ```sudo docker pull``` command
    5. Restart services.

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

Create another rule for port 3000 using the same way.

You are almost done. Now in Compute Engine > VM Instances select the one you want to use. Pick Edit at the top. Go to network tags and add "allow-8080" and "allow-3000". Save. 

You are ready to deploy on this VM.

## Verifying once set up

From outside try:
- http://dev.api.cleanapp.io:8080/help
- http://dev.app.cleanapp.io:3000/help

Both times you will get a plain short welcome message with CleanApp API/APP version. Remove ```dev.``` prefix for prod instance.

# Docker & DockerHub

Docker image:
```
<DOCKER_PREFIX>/cleanappapp:<DOCKER_LABEL>
```
e.g.
```
ibnazer/cleanappapp:1.6
```
*(1.6 is 2.0.alpha)*

When building an image update the script to build it ./dockerapp/build_server_image.sh: at the top there are two environment
variables: DOCKER_LABEL and DOCKER_PREFIX.

> **IMPORTANT:** When you build your new image your Docker client must run.

> **IMPORTANT:** When you push your new image to DockerHub, you must own the prefix.

# Google Cloud VM Instances

## Configuration
We picked

* E2 Low cost, day-to-day computing
* US-Central1 Iowa
* e2-medium (2 vCPU, 1 core, 4 GB memory)
* 10Gb Disk
* ubuntu-2004-focal-v20231101
  *Canonical, Ubuntu, 20.04 LTS, amd64 focal image built on 2023-11-01
* HTTP/HTTPS allowed.

## Our VM instances

* **cleanapp-1** Dev instance, http://dev.api.cleanapp.io / http://dev.app.cleanapp.io point to this instance (external IP 34.132.121.53).
* **cleanapp-prod** Prod instance, http://api.cleanapp.io / http://app.cleanapp.io point to this instance (external IP 35.184.156.86)

## More

More infro is to be added.
