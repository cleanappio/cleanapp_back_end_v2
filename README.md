# Cleanapp Backend version 2+

This repository is for CleanApp (http://cleanapp.io) backend development.

## Upload Docker image

Currently we're using personal free of charge DockerHub repositories for simplicity and money saving.

### Preparation
1.  Make sure docker is installed on your machine.
1.  Make sure you have an account on Docker hub.
1.  Create the .env file in the project root directory if it doesn't exist.
1.  Put the following content into there:
    ```
    DOCKER_PREFIX=eko2000
    DOCKER_LABEL=2
    ```

### Login to Dockerhub
1.  Set up the access token on your Docker account.
1.  Run login
    ```
    docker login -u <dockerhub_user_name>
    ```

### Docker image building
1.  Make sure the docker daemon is running on your machine.
1.  Run the build_server_image.sh
    ```
    cd docker && ./build_server_image.sh
    ```

### Cloud Deployment

Pre-requisites: Linux (Debian/Ubuntu/...), this is tested on Google Drive Ubuntu VPS instance.

1. Login to the target machine.
   * On GCloud you go to the dashboard, pick the instance, and the click on SSH
1. Get setup.sh into the current directory, e.g. using
```shell
curl https://raw.githubusercontent.com/cleanappio/cleanapp_back_end_v2/main/setup/setup.sh > setup.sh
```
1. Run
```
./setup.sh
```
1. When doing it, you will be asked to set specific docker and mysql settings:

* DOCKER_PREFIX
* DOCKER_LABEL
* MYSQL_ROOT_PASSWORD for MySQL root user password.
* MYSQL_APP_PASSWORD for MySQL password for the API server.
* MYSQL_READER_PASSWORD for MySQL password for database reading/import.

It should be up and running now. If not, contact eldarm@cleanapp.io

### Operations

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

### Direct dependencies

Docker images:
1. API server: &lt;dockerusername&gt;/cleanappserver:&lt;label&gt;
2. Database: &lt;dockerusername&gt;/cleanappdb:&lt;label&gt;
3. web application: &lt;dockerusername&gt;/cleanappapp:&lt;label&gt;

### Open ports

* API server exposes port 8080.
* APP server exposes port 3000.
* MySQL DB uses port 3306 but currently does not expose it externally. Do so,
if you want to connect to it from outside.

#### How to expose port in Google Cloud

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

### Verifying once set up

From outside try:
- http://dev.api.cleanapp.io:8080/help
- http://dev.app.cleanapp.io:3000/help

Both times you will get a plain short welcome message with CleanApp API/APP version. Remove ```dev.``` prefix for prod instance.

# Google Cloud VM Configuration

## Our VM instances

* **cleanapp-1** Dev instance, http://dev.api.cleanapp.io / http://dev.app.cleanapp.io point to this instance (external IP 34.132.121.53).
* **cleanapp-prod** Prod instance, http://api.cleanapp.io / http://app.cleanapp.io point to this instance (external IP 35.184.156.86)

## More

More infro is to be added.

