# Cleanapp Backend version 2+

This repository is for CleanApp (http://cleanapp.io) backend development.

# Environments
There are three environments:
*   `local` - a local machine outside cloud
*   `dev` - development machine in cloud
*   `prod` - production machine in cloud

# Installation

## Pre-requisites

1.  Make sure that your local machine has Docker installed. https://docs.docker.com/engine/install/
1.  Make sure you're prepared for working with GoogleCloud.
    1.  You got necessary access to Google Cloud services. Ask project admins for them.
    1.  You have gcloud command line interface installed, https://cloud.google.com/sdk/docs/install
    1.  You are successfully logged in gcloud, https://cloud.google.com/sdk/gcloud/reference/auth/login

## Installation steps

1.  Build docker images on your local machine.
1.  Deploy services on the cloud or local machine.

### Build Docker images for cleanapp backend

1.  Modify the Docker image version if necessary. Open the file `docker_backend/.version` and set the desired value of the `BUILD_VERSION`.
1.  Run the `docker_backend/build_server_image.sh` from the `docker_backens` directory.
    ```
    cd docker_backend &&
    ./build_image.sh
    ```

### Build Docker images for cleanapp referrals processing

1.  Modify the Docker image version if necessary. Open the file `docker_referrals/.version` and set the desired value of the `BUILD_VERSION`.
1.  Run the `docker_backend/build_server_image.sh` from the `docker_referrals` directory.
    ```
    cd docker_referrals &&
    ./build_image.sh
    ```

### Deploying in Google Cloud

Pre-requisites

*   Linux (Debian/Ubuntu/...), this is tested on Google Cloud Ubuntu VPS instance.
*   Make sure that gcloud is present on the cloud machine. It should be pre-installed by google cloud.
*   for installing on your local machine make sure that you installed gcloud.

1. Login to the target machine.
   * On GCloud you go to the dashboard, pick the instance, and the click on SSH
1. Get setup.sh into the current directory, e.g. using
```shell
curl https://raw.githubusercontent.com/cleanappio/cleanapp_back_end_v2/main/setup/setup.sh > setup.sh &&
sudo chmod a+x setup.sh
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

# Google Cloud VM Instances

## Machine Configuration
We picked

* E2 Low cost, day-to-day computing
* US-Central1 Iowa
* e2-medium (2 vCPU, 1 core, 4 GB memory)
* 10Gb Disk
* ubuntu-2004-focal-v20231101
  *Canonical, Ubuntu, 20.04 LTS, amd64 focal image built on 2023-11-01
* HTTP/HTTPS allowed.

## Secrets Setup
Currently we have three secrets per environment:
*   MYSQL_APP_PASSWORD_&lt;env&gt;
*   MYSQL_READER_PASSWORD_&lt;env&gt;
*   MYSQL_ROOT_PASSWORD_&lt;env&gt;
where &lt;env&gt; is `LOCAL`, `DEV` or `PROD`.

## Domains and Machines

* **cleanapp-1** Dev instance, http://dev.api.cleanapp.io / http://dev.app.cleanapp.io point to this instance (external IP 34.132.121.53).
* **cleanapp-prod** Prod instance, http://api.cleanapp.io / http://app.cleanapp.io point to this instance (TODO: Create the machine and edit DNS)

## More

More infro is to be added.
