# Deploying Backend Services

## Quick Deploy (Most Common)

For any backend service (email-service, report-analyze-pipeline, etc.):

```bash
cd /Users/anon16/Downloads/cleanapp_back_end_v2/<service-name>

# 1. Commit and push changes to GitHub
git add -A && git commit -m "feat: your commit message"
git push origin main

# 2. Deploy to DEV first (this BUILDS the new Docker image)
// turbo
./build_image.sh -e dev --ssh-keyfile ~/.ssh/id_ed25519

# 3. Deploy to PROD (this tags the dev image as prod and deploys)
./build_image.sh -e prod --ssh-keyfile ~/.ssh/id_ed25519
```

## Critical Notes

> [!IMPORTANT]
> **Always deploy to DEV first!**
> The `build_image.sh` script only builds a new Docker image when `-e dev` is used.
> For `-e prod`, it just re-tags the existing image. So you MUST deploy to dev first to get the new code built.

> [!WARNING]
> **Database migrations must be done BEFORE deploying code that uses new columns/tables.**
> If your code references new DB schema, add the columns/tables first via:
> ```bash
> gcloud compute ssh cleanapp-prod --zone=us-central1-a --command='docker exec cleanapp_db mysql ...'
> ```

## SSH Key Location

The SSH key for deployment is at: `~/.ssh/id_ed25519`

## Service Locations

| Service | Directory |
|---------|-----------|
| Email Service | `/Users/anon16/Downloads/cleanapp_back_end_v2/email-service` |
| Report Analyze Pipeline | `/Users/anon16/Downloads/cleanapp_back_end_v2/report-analyze-pipeline` |
| Backend Server | `/Users/anon16/Downloads/cleanapp_back_end_v2/backend` |

## VM IPs

| Environment | VM | IP |
|-------------|----|----|
| Dev | cleanapp-dev | 34.132.121.53 |
| Prod | cleanapp-prod | 34.122.15.16 |
