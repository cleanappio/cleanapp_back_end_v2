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

OPT=""
while [[ $# -gt 0 ]]; do
  case $1 in
    "-e"|"--env")
      OPT="$2"
      shift 2
      ;;
    "--ssh-keyfile")
      SSH_KEYFILE="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

# Choose the environment if not specified
if [ -z "${OPT}" ]; then
  echo "Usage: ./setup.sh -e <dev|prod>"
fi

case ${OPT} in
  "dev")
      echo "Using dev environment"
      CLEANAPP_HOST=34.132.121.53
      SCHEDULER_HOST="dev.api.cleanapp.io"
      ETH_NETWORK_URL_MAIN="https://sepolia.base.org"
      CONTRACT_ADDRESS_MAIN="0xDc41655b749E8F2922A6E5e525Fc04a915aEaFAA"
      PIPELINES_MAIN_PORT="8090"
      REACT_APP_REF_API_ENDPOINT="http://dev.api.cleanapp.io:8080/write_referral/"
      REACT_APP_EMAIL_CONSENT_API_ENDPOINT="https://devareas.cleanapp.io/api/v3/update_consent/"
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
      SEQ_START_FROM=1
      MONTENEGRO_AREA_ID=6779
      MONTENEGRO_AREA_SUB_IDS="6753,6754,6755,6757,6758,6759,6760,6761,6762,6763,6764,6765,6766,6767,6768,6769,6770,6778,6895,6910,6948,6951,6953,6954,6955"
      NEW_YORK_AREA_ID=6970
      NEW_YORK_AREA_SUB_IDS="6971,6972,6973,6974,6975"
      OPT_OUT_URL="http://dev.cleanapp.io/api/optout"
      FACE_DETECTOR_COUNT=10
      FACE_DETECTOR_HOST=34.68.94.220
      # FACE_DETECTOR_HOST=34.132.121.53
      FACE_DETECTOR_PORT_START=9500
      FACE_DETECTOR_INTERNAL_HOST=10.128.0.11
      FACE_DETECTOR_DEBUG=true
      REQUEST_REGISTRATOR_URL=https://stxn-cleanapp-dev.stxn.io:443
      DIGITAL_BASE_URL="https://dev.cleanapp.io/api/email"
      ENABLE_EMAIL_FETCHER="false"
      ENABLE_EMAIL_V3="false"
      # EPC pusher defaults (disabled by default)
      ENABLE_EPC_PUSHER="true"
      EPC_DISPATCH="true"
      EPC_REPORTS_START_SEQ="29604"
      ;;
  "prod")
      echo "Using prod environment"
      CLEANAPP_HOST=34.122.15.16
      SCHEDULER_HOST="api.cleanapp.io"
      ETH_NETWORK_URL_MAIN="https://sepolia.base.org"  # TODO: Change to the mainnet URL after we run on the base mainnet
      CONTRACT_ADDRESS_MAIN="0xDc41655b749E8F2922A6E5e525Fc04a915aEaFAA"  # TODO: Change the contract address to the main when we run on the base mainnet
      DISBURSEMENT_MAIN_SCHEDULE="*/3 * * * *"
      PIPELINES_MAIN_PORT="8090"
      REACT_APP_REF_API_ENDPOINT="http://api.cleanapp.io:8080/write_referral/"
      REACT_APP_EMAIL_CONSENT_API_ENDPOINT="https://areas.cleanapp.io/api/v3/update_consent/"
      SOLVER_URL="http://104.154.119.169:8888/report"
      CHAIN_ID="21363"
      WS_CHAIN_URL="wss://service.lestnet.org:8888/"
      LAMINATOR_ADDRESS="0x36aB7A6ad656BC19Da2D5Af5b46f3cf3fc47274D"
      CALL_BREAKER_ADDRESS="0x23912387357621473Ff6514a2DC20Df14cd72E7f"
      KITN_DISBURSEMENT_SCHEDULER_ADDRESS="0x7E485Fd55CEdb1C303b2f91DFE7695e72A537399"
      DISBURSEMENT_SHADOW_SCHEDULE="0 */3 * * * *"
      EMAIL_OPT_OUT_URL="http://app.cleanapp.io:3000"
      # Backend vars
      CLEANAPP_IO_TRUSTED_PROXIES=127.0.0.1,::1
      CLEANAPP_IO_BASE_URL=https://api.cleanapp.io
      STRIPE_PRICE_BASE_MONTHLY=price_1Rg0hJF5CkX59Cnm9L9Z4j36
      STRIPE_PRICE_BASE_ANNUAL=price_1Rg0hJF5CkX59CnmOyT5HZVu
      STRIPE_PRICE_ADVANCED_MONTHLY=price_1Rg0hEF5CkX59CnmT5ZspSPK
      STRIPE_PRICE_ADVANCED_ANNUAL=price_1Rg0hEF5CkX59CnmF40QClFx
      SEQ_START_FROM=24900
      MONTENEGRO_AREA_ID=6787
      MONTENEGRO_AREA_SUB_IDS="6761,6762,6763,6765,6766,6767,6768,6769,6770,6771,6772,6773,6774,6775,6776,6777,6778,6786,6903,6918,6956,6959,6961,6962,6963"
      NEW_YORK_AREA_ID=6636
      NEW_YORK_AREA_SUB_IDS="6637,6638,6639,6640,6641"
      GIN_MODE=release
      OPT_OUT_URL="https://cleanapp.io/api/optout"
      FACE_DETECTOR_COUNT=10
      FACE_DETECTOR_HOST=34.68.94.220
      FACE_DETECTOR_PORT_START=9500
      FACE_DETECTOR_INTERNAL_HOST=10.128.0.11
      FACE_DETECTOR_DEBUG=false
      REQUEST_REGISTRATOR_URL=https://stxn-cleanapp-prod.stxn.io:443
      DIGITAL_BASE_URL="https://cleanapp.io/api/email"
      ENABLE_EMAIL_FETCHER="false"
      ENABLE_EMAIL_V3="false"
      # EPC pusher defaults (disabled by default)
      ENABLE_EPC_PUSHER=""
      EPC_DISPATCH="false"
      EPC_REPORTS_START_SEQ=""
      ;;
  "quit")
      exit
      ;;
  *) echo "invalid option $REPLY";;
esac

SECRET_SUFFIX=$(echo ${OPT} | tr '[a-z]' '[A-Z]')

# Docker images
DOCKER_LOCATION="us-central1-docker.pkg.dev"
DOCKER_PREFIX="${DOCKER_LOCATION}/cleanup-mysql-v2/cleanapp-docker-repo"
SERVICE_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-service-image:${OPT}"
PIPELINES_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-pipelines-image:${OPT}"
WEB_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-web-image:${OPT}"
DB_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-db-image:live"
STXN_KICKOFF_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-stxn-kickoff-image:${OPT}"
CLEANAPP_IO_FRONTEND_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-frontend-image:${OPT}"
CLEANAPP_IO_FRONTEND_EMBEDDED_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-frontend-image-embedded:${OPT}"
CLEANAPP_IO_BACKEND_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-customer-service-image:${OPT}"
REPORT_LISTENER_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-report-listener-image:${OPT}"
REPORT_ANALYZE_PIPELINE_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-report-analyze-pipeline-image:${OPT}"
AREAS_DASHBOARD_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-custom-area-dashboard-image:${OPT}"
AUTH_SERVICE_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-auth-service-image:${OPT}"
BRAND_DASHBOARD_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-brand-dashboard-image:${OPT}"
AREAS_SERVICE_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-areas-service-image:${OPT}"
REPORT_PROCESSOR_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-report-processor-image:${OPT}"
EMAIL_SERVICE_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-email-service-image:${OPT}"
EMAIL_SERVICE_V3_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-email-service-v3-image:${OPT}"
EMAIL_FETCHER_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-email-fetcher-image:${OPT}"
REPORT_OWNERSHIP_SERVICE_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-report-ownership-service-image:${OPT}"
GDPR_PROCESS_SERVICE_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-gdpr-process-service-image:${OPT}"
REPORTS_PUSHER_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-reports-pusher-image:${OPT}"
FACE_DETECTOR_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-face-detector-image:${OPT}"
VOICE_ASSISTANT_SERVICE_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-voice-assistant-service-image:${OPT}"
REPORT_ANALYSIS_BACKFILL_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-report-analysis-backfill-image:${OPT}"
EPC_PUSHER_DOCKER_IMAGE="${DOCKER_PREFIX}/cleanapp-epc-pusher-image:${OPT}"

OPENAI_ASSISTANT_ID="asst_kBtuzDRWNorZgw9o2OJTGOn0"

RED_BULL_BRAND_NAMES="Red Bull"

UP_CLEANAPP="up1.sh"
UP_FACE_DETECTOR="up2.sh"
DOWN_CLEANAPP="down1.sh"
DOWN_FACE_DETECTOR="down2.sh"

# Create necessary files.
cat >up1.sh << UP
# Turn up CleanApp service.
# Assumes dependencies are in place (docker)

# Login to Artifact Registry (token embedded at generation time)
ACCESS_TOKEN=\$(gcloud auth print-access-token)
echo "\${ACCESS_TOKEN}" | docker login -u oauth2accesstoken --password-stdin https://${DOCKER_LOCATION}

docker pull ${SERVICE_DOCKER_IMAGE}
docker pull ${PIPELINES_DOCKER_IMAGE}
docker pull ${DB_DOCKER_IMAGE}
docker pull ${WEB_DOCKER_IMAGE}
docker pull ${STXN_KICKOFF_DOCKER_IMAGE}
docker pull ${CLEANAPP_IO_FRONTEND_DOCKER_IMAGE}
docker pull ${CLEANAPP_IO_FRONTEND_EMBEDDED_DOCKER_IMAGE}
docker pull ${CLEANAPP_IO_BACKEND_DOCKER_IMAGE}
docker pull ${REPORT_LISTENER_DOCKER_IMAGE}
docker pull ${REPORT_ANALYZE_PIPELINE_DOCKER_IMAGE}
docker pull ${AREAS_DASHBOARD_DOCKER_IMAGE}
docker pull ${AUTH_SERVICE_DOCKER_IMAGE}
docker pull ${BRAND_DASHBOARD_DOCKER_IMAGE}
docker pull ${AREAS_SERVICE_DOCKER_IMAGE}
docker pull ${REPORT_PROCESSOR_DOCKER_IMAGE}
docker pull ${EMAIL_SERVICE_DOCKER_IMAGE}
docker pull ${REPORT_OWNERSHIP_SERVICE_DOCKER_IMAGE}
docker pull ${GDPR_PROCESS_SERVICE_DOCKER_IMAGE}
docker pull ${REPORTS_PUSHER_DOCKER_IMAGE}
docker pull ${VOICE_ASSISTANT_SERVICE_DOCKER_IMAGE}
docker pull ${REPORT_ANALYSIS_BACKFILL_DOCKER_IMAGE}
docker pull ${EMAIL_SERVICE_V3_DOCKER_IMAGE}
docker pull ${EPC_PUSHER_DOCKER_IMAGE}
docker pull ${EMAIL_FETCHER_DOCKER_IMAGE}

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
TRASHFORMER_OPENAI_API_KEY=\$(gcloud secrets versions access 1 --secret="CLEANAPP_TRASHFORMER_OPENAI_API_KEY")
BLOCKSCAN_CHAT_API_KEY=\$(gcloud secrets versions access latest --secret="BLOCKSCAN_CHAT_API_KEY_${SECRET_SUFFIX}" --project cleanup-mysql-v2 | tr -d '\r' | sed -e 's/^"//' -e 's/"$//')

ENV

sudo docker compose --env-file .env up -d --remove-orphans

UP

if [[ "${OPT}" == "prod" ]]; then

cat >>up1.sh << UP1_TAIL

# Referrals redeem schedule
REFERRAL_SCHEDULER_NAME="referral-redeem-${OPT}"
EXISTING_REFERRAL_SCHEDULER=\$(gcloud scheduler jobs list --location=us-central1 | grep \${REFERRAL_SCHEDULER_NAME} | awk '{print \$1}')

if [[ "\${REFERRAL_SCHEDULER_NAME}" == "\${EXISTING_REFERRAL_SCHEDULER}" ]]; then
  gcloud scheduler jobs delete \${REFERRAL_SCHEDULER_NAME} \
    --location=us-central1 \
    --quiet
fi

gcloud scheduler jobs create http \${REFERRAL_SCHEDULER_NAME} \
  --location=us-central1 \
  --schedule="0 16 * * *" \
  --uri="http://${SCHEDULER_HOST}:${PIPELINES_MAIN_PORT}/referrals_redeem" \
  --message-body="{\"version\": \"2.0\"}" \
  --headers="Content-Type=application/json"

# Tokens disbursement schedule
DISBURSEMENT_MAIN_SCHEDULER_NAME="tokens-disburse-${OPT}"
EXISTING_DISBURSEMENT_MAIN_SCHEDULER=\$(gcloud scheduler jobs list --location=us-central1 | grep \${DISBURSEMENT_MAIN_SCHEDULER_NAME} | awk '{print \$1}')

if [[ "\${DISBURSEMENT_MAIN_SCHEDULER_NAME}" == "\${EXISTING_DISBURSEMENT_MAIN_SCHEDULER}" ]]; then
  gcloud scheduler jobs delete \${DISBURSEMENT_MAIN_SCHEDULER_NAME} \
    --location=us-central1 \
    --quiet
fi

gcloud scheduler jobs create http \${DISBURSEMENT_MAIN_SCHEDULER_NAME} \
  --location=us-central1 \
  --schedule="${DISBURSEMENT_MAIN_SCHEDULE}" \
  --uri="http://${SCHEDULER_HOST}:${PIPELINES_MAIN_PORT}/tokens_disburse" \
  --message-body="{\"version\": \"2.0\"}" \
  --headers="Content-Type=application/json"

UP1_TAIL
fi

chmod a+x up1.sh

cat >down1.sh << DOWN
# Turn down CleanApp service.
sudo docker compose down
# To clean up the database:
# sudo docker compose down -v
DOWN
chmod a+x down1.sh

cat >up2.sh << UP2

docker pull ${FACE_DETECTOR_DOCKER_IMAGE}

sudo docker compose -f docker-compose-face-detector.yml up -d --remove-orphans
UP2

chmod a+x up2.sh

cat >down2.sh << DOWN2
sudo docker compose -f docker-compose-face-detector.yml down
DOWN2

chmod a+x down2.sh

# Create docker-compose.yml file.
cat >docker-compose.yml << COMPOSE
version: '3'

services:
  cleanapp_rabbitmq:
    image: rabbitmq:latest
    container_name: cleanapp_rabbitmq
    restart: always
    ports:
      - 5672:5672
      - 15672:15672
    environment:
      RABBITMQ_DEFAULT_USER: cleanapp
      RABBITMQ_DEFAULT_PASS: cleanapp
    configs:
      - source: rabbitmq-plugins
        target: /etc/rabbitmq/enabled_plugins
    volumes:
      - rabbitmq-lib:/var/lib/rabbitmq/
      - rabbitmq-log:/var/log/rabbitmq

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
      - CLEANAPP_MAP_URL=${CLEANAPP_MAP_URL}
      - CLEANAPP_ANDROID_URL=${REACT_APP_PLAYSTORE_URL}
      - CLEANAPP_IOS_URL=${REACT_APP_APPSTORE_URL}
      - REPORT_ANALYSIS_URL=http://cleanapp_report_analyze_pipeline:8080
      - GIN_MODE=${GIN_MODE}
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
      - GIN_MODE=${GIN_MODE}
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

  cleanapp_frontend_embedded:
    container_name: cleanapp_frontend_embedded
    image: ${CLEANAPP_IO_FRONTEND_EMBEDDED_DOCKER_IMAGE}
    ports:
      - 3002:3000

  cleanapp_customer_service:
    container_name: cleanapp_customer_service
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
      - AUTH_SERVICE_URL=http://cleanapp_auth_service:8080
      - GIN_MODE=${GIN_MODE}
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
      - GIN_MODE=${GIN_MODE}
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
      - OPENAI_ASSISTANT_ID=${OPENAI_ASSISTANT_ID}
      - OPENAI_MODEL=gpt-4o
      - ANALYSIS_INTERVAL=500ms
      - MAX_RETRIES=3
      - ANALYSIS_PROMPT=${ANALYSIS_PROMPT}
      - LOG_LEVEL=info
      - SEQ_START_FROM=${SEQ_START_FROM}
      - GIN_MODE=${GIN_MODE}
    ports:
      - 9082:8080
    depends_on:
      - cleanapp_db

  cleanapp_montenegro_areas:
    container_name: cleanapp_montenegro_areas
    image: ${AREAS_DASHBOARD_DOCKER_IMAGE}
    environment:
      - DB_HOST=cleanapp_db
      - DB_PORT=3306
      - DB_USER=server
      - DB_PASSWORD=\${MYSQL_APP_PASSWORD}
      - DB_NAME=cleanapp
      - LOG_LEVEL=info
      - LOG_FORMAT=json
      - AUTH_SERVICE_URL=http://cleanapp_auth_service:8080
      - REPORT_AUTH_SERVICE_URL=http://cleanapp_report_auth_service:8080
      - GIN_MODE=${GIN_MODE}
      - CUSTOM_AREA_ID=${MONTENEGRO_AREA_ID}
      - CUSTOM_AREA_SUB_IDS=${MONTENEGRO_AREA_SUB_IDS}
    ports:
      - 9083:8080
    depends_on:
      - cleanapp_db

  cleanapp_new_york_areas:
    container_name: cleanapp_new_york_areas
    image: ${AREAS_DASHBOARD_DOCKER_IMAGE}
    environment:
      - DB_HOST=cleanapp_db
      - DB_PORT=3306
      - DB_USER=server
      - DB_PASSWORD=\${MYSQL_APP_PASSWORD}
      - DB_NAME=cleanapp
      - LOG_LEVEL=info
      - LOG_FORMAT=json
      - AUTH_SERVICE_URL=http://cleanapp_auth_service:8080
      - REPORT_AUTH_SERVICE_URL=http://cleanapp_report_auth_service:8080
      - GIN_MODE=${GIN_MODE}
      - CUSTOM_AREA_ID=${NEW_YORK_AREA_ID}
      - CUSTOM_AREA_SUB_IDS=${NEW_YORK_AREA_SUB_IDS}
    ports:
      - 9088:8080
    depends_on:
      - cleanapp_db

  cleanapp_auth_service:
    container_name: cleanapp_auth_service
    image: ${AUTH_SERVICE_DOCKER_IMAGE}
    environment:
      - TRUSTED_PROXIES=${CLEANAPP_IO_TRUSTED_PROXIES}
      - ENCRYPTION_KEY=\${CLEANAPP_IO_ENCRYPTION_KEY}
      - JWT_SECRET=\${CLEANAPP_IO_JWT_SECRET}
      - DB_HOST=cleanapp_db
      - DB_PORT=3306
      - DB_USER=server
      - DB_PASSWORD=\${MYSQL_APP_PASSWORD}
      - DB_NAME=cleanapp
      - GIN_MODE=${GIN_MODE}
    ports:
      - 9084:8080
    depends_on:
      - cleanapp_db

  cleanapp_red_bull_dashboard:
    container_name: cleanapp_red_bull_dashboard
    image: ${BRAND_DASHBOARD_DOCKER_IMAGE}
    environment:
      - DB_HOST=cleanapp_db
      - DB_PORT=3306
      - DB_USER=server
      - DB_PASSWORD=\${MYSQL_APP_PASSWORD}
      - DB_NAME=cleanapp
      - GIN_MODE=${GIN_MODE}
      - AUTH_SERVICE_URL=http://cleanapp_auth_service:8080
      - REPORT_AUTH_SERVICE_URL=http://cleanapp_report_auth_service:8080
      - BRAND_NAMES=${RED_BULL_BRAND_NAMES}
    ports:
      - 9085:8080

  cleanapp_areas_service:
    container_name: cleanapp_areas_service
    image: ${AREAS_SERVICE_DOCKER_IMAGE}
    environment:
      - DB_HOST=cleanapp_db
      - DB_PORT=3306
      - DB_USER=server
      - DB_PASSWORD=\${MYSQL_APP_PASSWORD}
      - DB_NAME=cleanapp
      - AUTH_SERVICE_URL=http://cleanapp_auth_service:8080
      - GIN_MODE=${GIN_MODE}
    ports:
      - 9086:8080

  cleanapp_report_processor:
    container_name: cleanapp_report_processor
    image: ${REPORT_PROCESSOR_DOCKER_IMAGE}
    environment:
      - DB_HOST=cleanapp_db
      - DB_PORT=3306
      - DB_USER=server
      - DB_PASSWORD=\${MYSQL_APP_PASSWORD}
      - DB_NAME=cleanapp
      - AUTH_SERVICE_URL=http://cleanapp_auth_service:8080
      - REPORT_AUTH_SERVICE_URL=http://cleanapp_report_auth_service:8080
      - REPORTS_SUBMISSION_URL=http://cleanapp_service:8080
      - OPENAI_API_KEY=\${OPENAI_API_KEY}
      - REPORTS_RADIUS_METERS=35.0
      - GIN_MODE=${GIN_MODE}
    ports:
      - 9087:8080

  cleanapp_email_service:
    container_name: cleanapp_email_service
    image: ${EMAIL_SERVICE_DOCKER_IMAGE}
    environment:
      - DB_HOST=cleanapp_db
      - DB_PORT=3306
      - DB_USER=server
      - DB_PASSWORD=\${MYSQL_APP_PASSWORD}
      - DB_NAME=cleanapp
      - SENDGRID_API_KEY=\${SENDGRID_API_KEY}
      - SENDGRID_FROM_NAME=${SENDGRID_FROM_NAME}
      - SENDGRID_FROM_EMAIL=${SENDGRID_FROM_EMAIL}
      - HTTP_PORT=8080
      - POLL_INTERVAL=10s
      - OPT_OUT_URL=${OPT_OUT_URL}
      - GIN_MODE=${GIN_MODE}
    ports:
      - 9089:8080
    depends_on:
      - cleanapp_db

  cleanapp_email_service_v3:
    container_name: cleanapp_email_service_v3
    image: ${EMAIL_SERVICE_V3_DOCKER_IMAGE}
    environment:
      - DB_HOST=cleanapp_db
      - DB_PORT=3306
      - DB_USER=server
      - DB_PASSWORD=\${MYSQL_APP_PASSWORD}
      - DB_NAME=cleanapp
      - SENDGRID_API_KEY=\${SENDGRID_API_KEY}
      - SENDGRID_FROM_NAME=${SENDGRID_FROM_NAME}
      - SENDGRID_FROM_EMAIL=${SENDGRID_FROM_EMAIL}
      - HTTP_PORT=8080
      - POLL_INTERVAL=10s
      - OPT_OUT_URL=${OPT_OUT_URL}
      - NOTIFICATION_PERIOD=90d
      - DIGITAL_BASE_URL=${DIGITAL_BASE_URL}
      - BCC_EMAIL_ADDRESS=cleanapp@stxn.io
      - ENABLE_EMAIL_V3=${ENABLE_EMAIL_V3}
    depends_on:
      - cleanapp_db

  cleanapp_email_fetcher:
    container_name: cleanapp_email_fetcher
    image: ${EMAIL_FETCHER_DOCKER_IMAGE}
    environment:
      - DB_HOST=cleanapp_db
      - DB_PORT=3306
      - DB_USER=server
      - DB_PASSWORD=${MYSQL_APP_PASSWORD}
      - DB_NAME=cleanapp
      - OPENAI_API_KEY=${OPENAI_API_KEY}
      - OPENAI_MODEL=gpt-4o
      - LOOP_DELAY_MS=10000
      - BATCH_LIMIT=10
      - ENABLE_EMAIL_FETCHER=${ENABLE_EMAIL_FETCHER}
    depends_on:
      - cleanapp_db

  cleanapp_report_ownership_service:
    container_name: cleanapp_report_ownership_service
    image: ${REPORT_OWNERSHIP_SERVICE_DOCKER_IMAGE}
    environment:
      - DB_HOST=cleanapp_db
      - DB_PORT=3306
      - DB_USER=server
      - DB_PASSWORD=\${MYSQL_APP_PASSWORD}
      - DB_NAME=cleanapp
      - POLL_INTERVAL=1s
      - BATCH_SIZE=100
      - GIN_MODE=${GIN_MODE}
    ports:
      - 9090:8080

  cleanapp_reports_pusher:
    container_name: cleanapp_reports_pusher
    image: ${REPORTS_PUSHER_DOCKER_IMAGE}
    environment:
      - MYSQL_URL=mysql://server:\${MYSQL_APP_PASSWORD}@cleanapp_db:3306/cleanapp
      - REQUEST_REGISTRATOR_URL=${REQUEST_REGISTRATOR_URL}
      - APP_ID_HEX=0000000000000000000000000000000000000000000000000000000000000000
      - CHAIN_ID=${CHAIN_ID}
      - POLL_SECS=5
    depends_on:
      - cleanapp_db

  cleanapp_gdpr_process_service:
    container_name: cleanapp_gdpr_process_service
    image: ${GDPR_PROCESS_SERVICE_DOCKER_IMAGE}
    environment:
      - DB_HOST=cleanapp_db
      - DB_PORT=3306
      - DB_USER=server
      - DB_PASSWORD=\${MYSQL_APP_PASSWORD}
      - DB_NAME=cleanapp
      - OPENAI_API_KEY=\${OPENAI_API_KEY}
      - OPENAI_MODEL=gpt-4o
      - FACE_DETECTOR_COUNT=${FACE_DETECTOR_COUNT}
      - FACE_DETECTOR_URL=http://${FACE_DETECTOR_INTERNAL_HOST}
      - FACE_DETECTOR_PORT_START=${FACE_DETECTOR_PORT_START}
      - POLL_INTERVAL=500ms
      - GIN_MODE=${GIN_MODE}
    ports:
      - 9091:8080
    depends_on:
      - cleanapp_db
  
  cleanapp_report_analysis_backfill:
    container_name: cleanapp_report_analysis_backfill
    image: ${REPORT_ANALYSIS_BACKFILL_DOCKER_IMAGE}
    environment:
      - DB_HOST=cleanapp_db
      - DB_PORT=3306
      - DB_USER=server
      - DB_PASSWORD=\${MYSQL_APP_PASSWORD}
      - DB_NAME=cleanapp
      - REPORT_ANALYSIS_URL=http://cleanapp_report_analyze_pipeline:8080
      - POLL_INTERVAL=1m
      - BATCH_SIZE=30
      - SEQ_END_TO=30000

  # Optional EPC pusher
  ${ENABLE_EPC_PUSHER:+cleanapp_epc_pusher:}
  ${ENABLE_EPC_PUSHER:+  container_name: cleanapp_epc_pusher}
  ${ENABLE_EPC_PUSHER:+  image: ${EPC_PUSHER_DOCKER_IMAGE}}
  ${ENABLE_EPC_PUSHER:+  restart: unless-stopped}
  ${ENABLE_EPC_PUSHER:+  environment:}
  ${ENABLE_EPC_PUSHER:+    - DB_HOST=cleanapp_db}
  ${ENABLE_EPC_PUSHER:+    - DB_PORT=3306}
  ${ENABLE_EPC_PUSHER:+    - DB_USER=server}
  ${ENABLE_EPC_PUSHER:+    - DB_PASSWORD=\${MYSQL_APP_PASSWORD}}
  ${ENABLE_EPC_PUSHER:+    - MYSQL_APP_PASSWORD=\${MYSQL_APP_PASSWORD}}
  ${ENABLE_EPC_PUSHER:+    - DB_NAME=cleanapp}
  ${ENABLE_EPC_PUSHER:+    - BLOCKSCAN_CHAT_API_KEY=\${BLOCKSCAN_CHAT_API_KEY}}
  ${ENABLE_EPC_PUSHER:+    - EPC_CONTRACT_ADDRESS=${CONTRACT_ADDRESS_MAIN}}
  ${ENABLE_EPC_PUSHER:+    - EPC_DISPATCH=${EPC_DISPATCH}}
  ${ENABLE_EPC_PUSHER:+    - EPC_REPORTS_START_SEQ=${EPC_REPORTS_START_SEQ}}
  ${ENABLE_EPC_PUSHER:+    - EPC_ONLY_VALID=${EPC_ONLY_VALID}}
  ${ENABLE_EPC_PUSHER:+    - EPC_FILTER_LANGUAGE=${EPC_FILTER_LANGUAGE}}
  ${ENABLE_EPC_PUSHER:+    - EPC_FILTER_SOURCE=${EPC_FILTER_SOURCE}}
  ${ENABLE_EPC_PUSHER:+  env_file:}
  ${ENABLE_EPC_PUSHER:+    - .env}
  ${ENABLE_EPC_PUSHER:+  depends_on:}
  ${ENABLE_EPC_PUSHER:+    - cleanapp_db}
  ${ENABLE_EPC_PUSHER:+  links:}
  ${ENABLE_EPC_PUSHER:+    - cleanapp_db}

  cleanapp_voice_assistant_service:
    container_name: cleanapp_voice_assistant_service
    image: ${VOICE_ASSISTANT_SERVICE_DOCKER_IMAGE}
    environment:
      - PORT=8080
      - TRASHFORMER_OPENAI_API_KEY=\${TRASHFORMER_OPENAI_API_KEY}
      - OPENAI_MODEL=gpt-4o-realtime-preview
      - RATE_LIMIT_PER_MINUTE=10
      - ALLOWED_ORIGINS=*
    ports:
      - 9092:8080

COMPOSE

FACE_DETECTOR_COUNT=10
FACE_DETECTOR_FILE="docker-compose.yml"

if [[ "${CLEANAPP_HOST}" != "${FACE_DETECTOR_HOST}" ]]; then
FACE_DETECTOR_FILE="docker-compose-face-detector.yml"
cat >${FACE_DETECTOR_FILE} << COMPOSE2_HEAD
services:

COMPOSE2_HEAD
fi

for ((i=1; i<=${FACE_DETECTOR_COUNT}; i++))
do
  cat >>${FACE_DETECTOR_FILE} << COMPOSE2_BODY
  cleanapp_face_detector_$i:
    container_name: cleanapp_face_detector_$i
    image: ${FACE_DETECTOR_DOCKER_IMAGE}
    restart: unless-stopped
    environment:
      - BLUR_STRENGTH=50
      - DEBUG=${FACE_DETECTOR_DEBUG}
      - RELOAD=false
      - ACCESS_LOG=true
    ports:
      - $((${FACE_DETECTOR_PORT_START} + $i)):8080

COMPOSE2_BODY
done

cat >>docker-compose.yml << COMPOSE_TAIL

configs:
  rabbitmq-plugins:
    content: "[rabbitmq_management]."  

volumes:
  mysql:
    name: eko_mysql
    external: true

  rabbitmq-lib:
    name: rabbitmq-lib
    driver: local

  rabbitmq-log:
    name: rabbitmq-log
    driver: local

COMPOSE_TAIL

set -e

# Copy files to target VM.
if [ -n "${SSH_KEYFILE}" ]; then
  scp -i ${SSH_KEYFILE} up1.sh down1.sh docker-compose.yml deployer@${CLEANAPP_HOST}:~/
else
  scp up1.sh down1.sh docker-compose.yml deployer@${CLEANAPP_HOST}:~/
fi
rm up1.sh down1.sh docker-compose.yml

if [[ "${CLEANAPP_HOST}" != "${FACE_DETECTOR_HOST}" ]]; then
  if [ -n "${SSH_KEYFILE}" ]; then
    scp -i ${SSH_KEYFILE} up2.sh down2.sh docker-compose-face-detector.yml deployer@${FACE_DETECTOR_HOST}:~/
  else
    scp up2.sh down2.sh docker-compose-face-detector.yml deployer@${FACE_DETECTOR_HOST}:~/
  fi
  rm up2.sh down2.sh docker-compose-face-detector.yml
fi

# Start docker containers.
if [ -n "${SSH_KEYFILE}" ]; then
  ssh -i ${SSH_KEYFILE} deployer@${CLEANAPP_HOST} "./up1.sh"
else
  ssh deployer@${CLEANAPP_HOST} "./up1.sh"
fi
if [[ "${CLEANAPP_HOST}" != "${FACE_DETECTOR_HOST}" ]]; then
  if [ -n "${SSH_KEYFILE}" ]; then
    ssh -i ${SSH_KEYFILE} deployer@${FACE_DETECTOR_HOST} "./up2.sh"
  else
    ssh deployer@${FACE_DETECTOR_HOST} "./up2.sh"
  fi
fi
