# Full Cleanapp.io setup on a clean Linux machine.
#
# Pre-reqs:
# 1. Linux machine: Debian/Ubuntu/...
# 2. setup.sh file from our setup folder locally in a local folder
#    (pulled from Github or otherwise).

# Vars init
SCHEDULER_HOST=
ETH_NETWORK_URL_MAIN=
CONTRACT_ADDRESS_MAIN=
DISBURSEMENT_MAIN_SCHEDULE=
PIPELINES_MAIN_PORT=
REACT_APP_REF_API_ENDPOINT=
SOLVER_URL=
DISBURSEMENT_SHADOW_SCHEDULE=
REACT_APP_EMAIL_CONSENT_API_ENDPOINT=

# STXN Kickoff Vars
CHAIN_ID=""
WS_CHAIN_URL=""
LAMINATOR_ADDRESS=""
CALL_BREAKER_ADDRESS=""
KITN_DISBURSEMENT_SCHEDULER_ADDRESS=""

# Cleanapp Web env variables
REACT_APP_PLAYSTORE_URL="https://play.google.com/store/apps/details?id=com.cleanapp"
REACT_APP_APPSTORE_URL="https://apps.apple.com/us/app/cleanapp/id6466403301"

# SendGrid Email Vars
SENDGRID_FROM_NAME="CleanApp"
SENDGRID_FROM_EMAIL="info@cleanapp.io"
EMAIL_OPT_OUT_URL=
CLEANAPP_MAP_URL="https://clean-app-map-4-b0150.replit.app/"


# Choose the environment
PS3="Please choose the environment: "
options=("dev" "prod" "quit")
select OPT in "${options[@]}"
do
  case ${OPT} in
    "dev")
        echo "Using dev environment"
        SCHEDULER_HOST="dev.api.cleanapp.io"
        ETH_NETWORK_URL_MAIN="https://sepolia.base.org"
        CONTRACT_ADDRESS_MAIN="0xDc41655b749E8F2922A6E5e525Fc04a915aEaFAA"
        PIPELINES_MAIN_PORT="8090"
        REACT_APP_REF_API_ENDPOINT="http://dev.api.cleanapp.io:8080/write_referral/"
        REACT_APP_EMAIL_CONSENT_API_ENDPOINT="http://dev.api.cleanapp.io:8080/update_consent/"
        SOLVER_URL="http://104.154.119.169:8888/report"
        CHAIN_ID="21363"
        WS_CHAIN_URL="wss://service.lestnet.org:8888/"
        LAMINATOR_ADDRESS="0x36aB7A6ad656BC19Da2D5Af5b46f3cf3fc47274D"
        CALL_BREAKER_ADDRESS="0x23912387357621473Ff6514a2DC20Df14cd72E7f"
        KITN_DISBURSEMENT_SCHEDULER_ADDRESS="0x7E485Fd55CEdb1C303b2f91DFE7695e72A537399"
        DISBURSEMENT_SHADOW_SCHEDULE="0 */3 * * * *"
        EMAIL_OPT_OUT_URL="http://dev.app.cleanapp.io:3000"
        # Backend vars
        CLEANAPP_IO_TRUSTED_PROXIES=127.0.0.1,::1
        CLEANAPP_IO_BASE_URL=https://devapi.cleanapp.io
        STRIPE_PRICE_BASE_MONTHLY=price_1ReIJJFW3SknKzLcejkfepTO
        STRIPE_PRICE_BASE_ANNUAL=price_1ReIJJFW3SknKzLcXOje9FBb
        STRIPE_PRICE_ADVANCED_MONTHLY=price_1ReIKiFW3SknKzLcaPTOR5Ny
        STRIPE_PRICE_ADVANCED_ANNUAL=price_1ReIKiFW3SknKzLcVMZe6U3U
        SEQ_START_FROM=1988
        break
        ;;
    "prod")
        echo "Using prod environment"
        SCHEDULER_HOST="api.cleanapp.io"
        ETH_NETWORK_URL_MAIN="https://sepolia.base.org"  # TODO: Change to the mainnet URL after we run on the base mainnet
        CONTRACT_ADDRESS_MAIN="0xDc41655b749E8F2922A6E5e525Fc04a915aEaFAA"  # TODO: Change the contract address to the main when we run on the base mainnet
        DISBURSEMENT_MAIN_SCHEDULE="0 20 * * *"
        PIPELINES_MAIN_PORT="8090"
        REACT_APP_REF_API_ENDPOINT="http://api.cleanapp.io:8080/write_referral/"
        REACT_APP_EMAIL_CONSENT_API_ENDPOINT="http://api.cleanapp.io:8080/update_consent/"
        SOLVER_URL="http://104.154.119.169:8888/report"
        CHAIN_ID="21363"
        WS_CHAIN_URL="wss://service.lestnet.org:8888/"
        LAMINATOR_ADDRESS="0x36aB7A6ad656BC19Da2D5Af5b46f3cf3fc47274D"
        CALL_BREAKER_ADDRESS="0x23912387357621473Ff6514a2DC20Df14cd72E7f"
        KITN_DISBURSEMENT_SCHEDULER_ADDRESS="0x7E485Fd55CEdb1C303b2f91DFE7695e72A537399"
        DISBURSEMENT_SHADOW_SCHEDULE="0 */3 * * * *"
        EMAIL_OPT_OUT_URL="http://app.cleanapp.io:3000"
        break
        ;;
    "quit")
        exit
        ;;
    *) echo "invalid option $REPLY";;
  esac
done

SECRET_SUFFIX=$(echo ${OPT} | tr '[a-z]' '[A-Z]')

# Create necessary files.
cat >up.sh << UP
# Turn up CleanApp service.
# Assumes dependencies are in place (docker)

# Secrets
cat >.env << ENV
MYSQL_ROOT_PASSWORD=\$(gcloud secrets versions access 1 --secret="MYSQL_ROOT_PASSWORD_${SECRET_SUFFIX}")
MYSQL_APP_PASSWORD=\$(gcloud secrets versions access 1 --secret="MYSQL_APP_PASSWORD_${SECRET_SUFFIX}")
MYSQL_READER_PASSWORD=\$(gcloud secrets versions access 1 --secret="MYSQL_READER_PASSWORD_${SECRET_SUFFIX}")
KITN_PRIVATE_KEY_MAIN=\$(gcloud secrets versions access 1 --secret="KITN_PRIVATE_KEY_${SECRET_SUFFIX}")
KITN_PRIVATE_KEY_SHADOW=\$(gcloud secrets versions access 1 --secret="KITN_PRIVATE_KEY_${SECRET_SUFFIX}")
SENDGRID_API_KEY=\$(gcloud secrets versions access 1 --secret="SENDGRID_API_KEY_${SECRET_SUFFIX}")
CLEANAPP_IO_ENCRYPTION_KEY=\$(gcloud secrets versions access 1 --secret="CLEANAPP_IO_ENCRYPTION_KEY_${SECRET_SUFFIX}")
CLEANAPP_IO_JWT_SECRET=\$(gcloud secrets versions access 1 --secret="CLEANAPP_IO_JWT_SECRET_${SECRET_SUFFIX}")
STRIPE_SECRET_KEY=\$(gcloud secrets versions access 1 --secret="STRIPE_SECRET_KEY_${SECRET_SUFFIX}")
STRIPE_WEBHOOK_SECRET=\$(gcloud secrets versions access 1 --secret="STRIPE_WEBHOOK_SECRET_${SECRET_SUFFIX}")
OPENAI_API_KEY=\$(gcloud secrets versions access 1 --secret="CLEANAPP_CHATGPT_API_KEY")

ENV

sudo docker-compose up -d --remove-orphans

rm -f .env

UP

sudo chmod a+x up.sh

cat >down.sh << DOWN
# Turn down CleanApp service.
sudo docker-compose down
# To clean up the database:
# sudo docker-compose down -v
DOWN
sudo chmod a+x down.sh

# Docker images
DOCKER_LOCATION="us-central1-docker.pkg.dev"
DOCKER_PREFIX="${DOCKER_LOCATION}/cleanup-mysql-v2/cleanapp-docker-repo"
SERVICE_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-service-image:${OPT}"
PIPELINES_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-pipelines-image:${OPT}"
WEB_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-web-image:${OPT}"
DB_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-db-image:live"
STXN_KICKOFF_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-stxn-kickoff-image:${OPT}"
CLEANAPP_IO_FRONTEND_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-frontend-image:${OPT}"
CLEANAPP_IO_BACKEND_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-customer-service-image:${OPT}"
REPORT_LISTENER_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-report-listener-image:${OPT}"
REPORT_ANALYZE_PIPELINE_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-report-analyze-pipeline-image:${OPT}"

# Create docker-compose.yml file.
cat >docker-compose.yml << COMPOSE
version: '3'

services:
  cleanapp_service:
    container_name: cleanapp_service
    image: ${SERVICE_DOCKER_IMAGE}
    environment:
      - MYSQL_ROOT_PASSWORD=\${MYSQL_ROOT_PASSWORD}
      - MYSQL_APP_PASSWORD=\${MYSQL_APP_PASSWORD}
      - KITN_PRIVATE_KEY_MAIN=\${KITN_PRIVATE_KEY_MAIN}
      - ETH_NETWORK_URL_MAIN=${ETH_NETWORK_URL_MAIN}
      - CONTRACT_ADDRESS_MAIN=${CONTRACT_ADDRESS_MAIN}
      - SOLVER_URL=${SOLVER_URL}
      - SENDGRID_API_KEY=\${SENDGRID_API_KEY}
      - SENDGRID_FROM_NAME=${SENDGRID_FROM_NAME}
      - SENDGRID_FROM_EMAIL=${SENDGRID_FROM_EMAIL}
      - EMAIL_OPT_OUT_URL=${EMAIL_OPT_OUT_URL}
      - CLEANAPP_MAP_URL=${CLEANAPP_MAP_URL}
      - CLEANAPP_ANDROID_URL=${REACT_APP_PLAYSTORE_URL}
      - CLEANAPP_IOS_URL=${REACT_APP_APPSTORE_URL}
    ports:
      - 8080:8080

  cleanapp_pipelines:
    container_name: cleanapp_pipelines
    image: ${PIPELINES_DOCKER_IMAGE}
    environment:
      - PIPELINES_PORT=${PIPELINES_MAIN_PORT}
      - MYSQL_ROOT_PASSWORD=\${MYSQL_ROOT_PASSWORD}
      - MYSQL_APP_PASSWORD=\${MYSQL_APP_PASSWORD}
      - KITN_PRIVATE_KEY=\${KITN_PRIVATE_KEY_MAIN}
      - ETH_NETWORK_URL=${ETH_NETWORK_URL_MAIN}
      - CONTRACT_ADDRESS=${CONTRACT_ADDRESS_MAIN}
      - USERS_TABLE=users
    ports:
      - ${PIPELINES_MAIN_PORT}:${PIPELINES_MAIN_PORT}

  cleanapp_db:
    container_name: cleanapp_db
    image: ${DB_DOCKER_IMAGE}
    environment:
      - MYSQL_ROOT_PASSWORD=\${MYSQL_ROOT_PASSWORD}
      - MYSQL_APP_PASSWORD=\${MYSQL_APP_PASSWORD}
      - MYSQL_READER_PASSWORD=\${MYSQL_READER_PASSWORD}
    volumes:
      - mysql:/var/lib/mysql
    ports:
      - 3306:3306

  cleanapp_web:
    container_name: cleanapp_web
    image: ${WEB_DOCKER_IMAGE}
    environment:
      - REACT_APP_REF_API_ENDPOINT=${REACT_APP_REF_API_ENDPOINT}
      - REACT_APP_EMAIL_CONSENT_API_ENDPOINT=${REACT_APP_EMAIL_CONSENT_API_ENDPOINT}
      - REACT_APP_PLAYSTORE_URL=${REACT_APP_PLAYSTORE_URL}
      - REACT_APP_APPSTORE_URL=${REACT_APP_APPSTORE_URL}
    ports:
      - 3000:3000

  cleanapp_stxn_kickoff:
    container_name: stxn_kickoff
    image: ${STXN_KICKOFF_DOCKER_IMAGE}
    environment:
      - CHAIN_ID=${CHAIN_ID}
      - WS_CHAIN_URL=${WS_CHAIN_URL}
      - LAMINATOR_ADDRESS=${LAMINATOR_ADDRESS}
      - CALL_BREAKER_ADDRESS=${CALL_BREAKER_ADDRESS}
      - KITN_DISBURSEMENT_SCHEDULER_ADDRESS=${KITN_DISBURSEMENT_SCHEDULER_ADDRESS}
      - CLEANAPP_WALLET_PRIVATE_KEY=\${KITN_PRIVATE_KEY_SHADOW}
      - DISBURSEMENT_SHADOW_SCHEDULE=${DISBURSEMENT_SHADOW_SCHEDULE}

  cleanapp_frontend:
    container_name: cleanapp_frontend
    image: ${CLEANAPP_IO_FRONTEND_DOCKER_IMAGE}
    ports:
      - 3001:3000

  cleanapp_backend:
    container_name: cleanapp_backend
    image: ${CLEANAPP_IO_BACKEND_DOCKER_IMAGE}
    environment:
      - TRUSTED_PROXIES=${CLEANAPP_IO_TRUSTED_PROXIES}
      - BASE_URL=${CLEANAPP_IO_BASE_URL}
      - STRIPE_SECRET_KEY=\${STRIPE_SECRET_KEY}
      - STRIPE_WEBHOOK_SECRET=\${STRIPE_WEBHOOK_SECRET}
      - ENCRYPTION_KEY=\${CLEANAPP_IO_ENCRYPTION_KEY}
      - JWT_SECRET=\${CLEANAPP_IO_JWT_SECRET}
      - STRIPE_PRICE_BASE_MONTHLY=${STRIPE_PRICE_BASE_MONTHLY}
      - STRIPE_PRICE_BASE_ANNUAL=${STRIPE_PRICE_BASE_ANNUAL}
      - STRIPE_PRICE_ADVANCED_MONTHLY=${STRIPE_PRICE_ADVANCED_MONTHLY}
      - STRIPE_PRICE_ADVANCED_ANNUAL=${STRIPE_PRICE_ADVANCED_ANNUAL}
      - DB_HOST=cleanapp_db
      - DB_PORT=3306
      - DB_USER=server
      - DB_PASSWORD=\${MYSQL_APP_PASSWORD}
    ports:
      - 9080:8080

  cleanapp_report_listener:
    container_name: cleanapp_report_listener
    image: ${REPORT_LISTENER_DOCKER_IMAGE}
    environment:
      - DB_HOST=cleanapp_db
      - DB_PORT=3306
      - DB_USER=server
      - DB_PASSWORD=\${MYSQL_APP_PASSWORD}
      - DB_NAME=cleanapp
      - BROADCAST_INTERVAL=1s
      - LOG_LEVEL=info
    ports:
      - 9081:8080
    depends_on:
      - cleanapp_db

  cleanapp_report_analyze_pipeline:
    container_name: cleanapp_report_analyze_pipeline
    image: ${REPORT_ANALYZE_PIPELINE_DOCKER_IMAGE}
    environment:
      - DB_HOST=cleanapp_db
      - DB_PORT=3306
      - DB_USER=server
      - DB_PASSWORD=\${MYSQL_APP_PASSWORD}
      - DB_NAME=cleanapp
      - OPENAI_API_KEY=\${OPENAI_API_KEY}
      - OPENAI_MODEL=gpt-4.1
      - ANALYSIS_INTERVAL=500ms
      - MAX_RETRIES=3
      - ANALYSIS_PROMPT="What kind of litter or hazard can you see on this image? Please describe the litter or hazard in detail. Also, give a probability that there is a litter or hazard on a photo and a severity level from 0.0 to 1.0."
      - LOG_LEVEL=info
      - SEQ_START_FROM=${SEQ_START_FROM}
    ports:
      - 9082:8080
    depends_on:
      - cleanapp_db

volumes:
  mysql:

COMPOSE

set -e

# Pull images:
docker pull ${SERVICE_DOCKER_IMAGE}
docker pull ${PIPELINES_DOCKER_IMAGE}
docker pull ${DB_DOCKER_IMAGE}
docker pull ${WEB_DOCKER_IMAGE}
docker pull ${STXN_KICKOFF_DOCKER_IMAGE}
docker pull ${CLEANAPP_IO_FRONTEND_DOCKER_IMAGE}
docker pull ${CLEANAPP_IO_BACKEND_DOCKER_IMAGE}
docker pull ${REPORT_LISTENER_DOCKER_IMAGE}
docker pull ${REPORT_ANALYZE_PIPELINE_DOCKER_IMAGE}

# Start our docker images.
./up.sh

# Skip scheduling for a local and dev environment.
if [[ "${OPT}" != "prod" ]]; then
  exit 0
fi

# Referrals redeem schedule
REFERRAL_SCHEDULER_NAME="referral-redeem-${OPT}"
EXISTING_REFERRAL_SCHEDULER=$(gcloud scheduler jobs list --location=us-central1 | grep ${REFERRAL_SCHEDULER_NAME} | awk '{print $1}')

if [[ "${REFERRAL_SCHEDULER_NAME}" == "${EXISTING_REFERRAL_SCHEDULER}" ]]; then
  gcloud scheduler jobs delete ${REFERRAL_SCHEDULER_NAME} \
    --location=us-central1 \
    --quiet
fi

gcloud scheduler jobs create http ${REFERRAL_SCHEDULER_NAME} \
  --location=us-central1 \
  --schedule="0 16 * * *" \
  --uri="http://${SCHEDULER_HOST}:${PIPELINES_MAIN_PORT}/referrals_redeem" \
  --message-body="{\"version\": \"2.0\"}" \
  --headers="Content-Type=application/json"

# Tokens disbursement schedule
DISBURSEMENT_MAIN_SCHEDULER_NAME="tokens-disburse-${OPT}"
EXISTING_DISBURSEMENT_MAIN_SCHEDULER=$(gcloud scheduler jobs list --location=us-central1 | grep ${DISBURSEMENT_MAIN_SCHEDULER_NAME} | awk '{print $1}')

if [[ "${DISBURSEMENT_MAIN_SCHEDULER_NAME}" == "${EXISTING_DISBURSEMENT_MAIN_SCHEDULER}" ]]; then
  gcloud scheduler jobs delete ${DISBURSEMENT_MAIN_SCHEDULER_NAME} \
    --location=us-central1 \
    --quiet
fi

gcloud scheduler jobs create http ${DISBURSEMENT_MAIN_SCHEDULER_NAME} \
  --location=us-central1 \
  --schedule="${DISBURSEMENT_MAIN_SCHEDULE}" \
  --uri="http://${SCHEDULER_HOST}:${PIPELINES_MAIN_PORT}/tokens_disburse" \
  --message-body="{\"version\": \"2.0\"}" \
  --headers="Content-Type=application/json"
