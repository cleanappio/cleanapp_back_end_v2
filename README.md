# CleanApp Backend version 2+

This repository is for CleanApp (http://cleanapp.io) backend development.

**If you want to understand CleanApp as a system, start here:**  
[WHY](./WHY.md) → [THEORY](./THEORY.md) → [INVARIANTS](./INVARIANTS.md) → [ARCHITECTURE](./ARCHITECTURE.md)

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

### Build Docker images for cleanapp referrals and token disbursement processing

1.  Modify the Docker image version if necessary. Open the file `docker_pipelines/.version` and set the desired value of the `BUILD_VERSION`.
1.  Run the `docker_backend/build_server_image.sh` from the `docker_pipelines` directory.
    ```
    cd docker_pipelines &&
    ./build_image.sh
    ```

### Deploying in Google Cloud

The deploying process includes:
*   pulling docker images and running four services:
    *   cleanapp backend service;
    *   cleanapp referral service;
    *   mysql database;
    *   cleanapp web service;
*   adding Google cloud scheduler for following processes:
    *   referrals redeem;
    *   tokens disbursement;

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
- http://dev.api.cleanapp.io:8090/help
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

* (update: December 15, 2025) - need to adjust 10Gb Disk upwards as web scraping needs increase (eg, Bluesky & upcoming RedditReader deployments), see Architecture.md 

---

# Deployment Guide (Updated December 2025)

## Quick Reference: Service Ports

### Core Services
| Service | Host Port | Container Port | Domain/Proxy |
|---------|-----------|----------------|--------------|
| cleanapp_service (main API) | 8080 | 8080 | api.cleanapp.io:8080 |
| cleanapp_pipelines | 8090 | 8090 | api.cleanapp.io:8090 |
| cleanapp_web (legacy) | 3000 | 3000 | - |
| cleanapp_frontend | 3001 | 3000 | cleanapp.io (nginx) |
| cleanapp_frontend_embedded | 3002 | 3000 | - |

### Report Processing Services
| Service | Host Port | Container Port | Notes |
|---------|-----------|----------------|-------|
| report_listener | 9081 | 8080 | Primary report API (live.cleanapp.io) |
| report_listener_v4 | 9097 | 8080 | Rust-based v4 API |
| report_analyze_pipeline | 9082 | 8080 | AI analysis pipeline |
| report_processor | 9087 | 8080 | Report processing |
| report_renderer_service | 9093 | 8080 | Image rendering |
| report_tags_service | 9098 | 8080 | Tag management |
| report_ownership_service | 9090 | 8080 | Ownership tracking |

### Authentication & Customer Services
| Service | Host Port | Container Port |
|---------|-----------|----------------|
| auth_service | 9084 | 8080 |
| customer_service | 9080 | 8080 |
| gdpr_process_service | 9091 | 8080 |
| voice_assistant_service | 9092 | 8080 |

### Area/Event Dashboards
| Service | Host Port | Container Port |
|---------|-----------|----------------|
| areas_service | 9086 | 8080 |
| devconnect_2025_areas | 9094 | 8080 |
| edge_city_areas | 9095 | 8080 |
| new_york_areas | 9088 | 8080 |
| montenegro_areas | 9083 | 8080 |
| red_bull_dashboard | 9085 | 8080 |

### Infrastructure
| Service | Host Port | Notes |
|---------|-----------|-------|
| cleanapp_db (MySQL) | 3306 | Primary database |
| cleanapp_rabbitmq | 5672, 15672 | Message queue |

### Background Services (No External Ports)
- `bluesky_indexer` - Indexes Bluesky posts
- `bluesky_analyzer` - Analyzes posts with Gemini AI
- `bluesky_submitter` - Submits analyzed posts to report_listener
- `bluesky_now` - Real-time Bluesky stream
- `news_indexer_twitter` - Twitter/X indexing
- `replier_twitter` - Twitter reply bot
- `email_fetcher` - Email processing

---

## Full Backend Deployment

Deploy all backend services to production:

```bash
cd setup
./setup.sh -e prod --ssh-keyfile ~/.ssh/id_ed25519
```

This will:
1. Pull all Docker images tagged as `:prod`
2. Run `docker compose up -d` with all services
3. Configure Cloud Scheduler jobs

For dev environment:
```bash
./setup.sh -e dev --ssh-keyfile ~/.ssh/id_ed25519
```

---

## Frontend Deployment

### Fast Deploy (Recommended for Frontend-Only Changes)
Builds and deploys just the frontend without touching other services:

```bash
cd cleanapp-frontend
./fastFEdeploy.sh -e prod
```

Takes ~7 minutes. Suitable for:
- UI/styling changes
- Component updates
- Configuration changes

### Full Frontend Deploy
Includes embedded frontend:

```bash
cd cleanapp-frontend
./build_images.sh -e prod --ssh-keyfile ~/.ssh/id_ed25519
```

---

## Individual Microservice Deployment

Each microservice can be built and deployed independently. General pattern:

### 1. Build the Image
```bash
cd <service-directory>
./build_image.sh -e dev   # Builds and pushes to registry
```

### 2. Tag for Production
```bash
gcloud artifacts docker tags add \
  us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/<image-name>:<version> \
  us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/<image-name>:prod
```

### 3. Deploy via setup.sh
```bash
cd setup && ./setup.sh -e prod --ssh-keyfile ~/.ssh/id_ed25519
```

### Key Microservices with Build Scripts

| Service | Directory | Notes |
|---------|-----------|-------|
| report-listener | `report-listener/` | Main Go API for reports |
| report-listener-v4 | `report-listener-v4/` | Rust v4 API |
| auth-service | `auth-service/` | Authentication |
| report-analyze-pipeline | `report-analyze-pipeline/` | AI analysis |
| news-indexer-bluesky | `news-indexer-bluesky/` | Bluesky indexer/analyzer/submitter |
| email-service-v3 | `email-service-v3/` | Email notifications |
| report-processor | `report-processor/` | Report processing |
| areas-service | `areas-service/` | Geo-areas API |
| customer-service | `customer-service/` | Customer management |
| face-detector | `face-detector/` | Privacy face detection |

---

## Bluesky Services

The Bluesky pipeline consists of 4 services:

```
bluesky_indexer → bluesky_analyzer → bluesky_submitter → report_listener
       ↓
  bluesky_now (real-time stream)
```

### Deploy Bluesky Services
```bash
cd news-indexer-bluesky
./build_images.sh -e prod
```

### Check Status
```bash
docker ps | grep bluesky
docker logs cleanapp_bluesky_indexer --tail 20
docker logs cleanapp_bluesky_analyzer --tail 20
docker logs cleanapp_bluesky_submitter --tail 20
```

### Restart Bluesky Services
```bash
docker start cleanapp_bluesky_indexer cleanapp_bluesky_analyzer cleanapp_bluesky_submitter
```

---

## Troubleshooting

### Check Service Health
```bash
# All running containers
docker ps --format "table {{.Names}}\t{{.Status}}"

# Service logs
docker logs <container_name> --tail 50

# Database connection
docker exec -it cleanapp_db mysql -u server -p cleanapp
```

### Restart a Single Service
```bash
docker stop <container_name>
docker rm <container_name>
docker compose up -d <service_name>
```

### Full System Restart
```bash
cd /home/deployer
docker compose down
docker compose up -d
```

---

## Nginx Reverse Proxy Domains

| Domain | Backend |
|--------|---------|
| cleanapp.io | :3001 (frontend) |
| live.cleanapp.io | :9081 (report_listener) |
| auth.cleanapp.io | :9084 (auth_service) |
| processing.cleanapp.io | :9087 (report_processor) |
| areas.cleanapp.io | :9086 (areas_service) |
| email.cleanapp.io | email-service |
| renderer.cleanapp.io | :9093 (report_renderer) |

---

## Version Management

Each service has a `.version` file containing `BUILD_VERSION=x.y.z`.

To bump version before building:
```bash
# Check current version
cat .version

# The build script auto-increments minor version
./build_image.sh -e dev
```

