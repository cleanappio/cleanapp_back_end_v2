# Full Cleanapp.io setup on a clean Linux machine.
#
# Pre-reqs:
# 1. Linux machine: Debian/Ubuntu/...
# 2. setup.sh file from our setup folder locally in a local folder
#    (pulled from Github or otherwise).

# Vars init
SCHEDULER_HOST=""
ETH_NETWORK_URL_MAIN=""
CONTRACT_ADDRESS_MAIN=""
DISBURSEMENT_MAIN_SCHEDULE=""
PIPELINES_MAIN_PORT=""
REACT_APP_REF_API_ENDPOINT="http://dev.api.cleanapp.io:8080/write_referral/"
SOLVER_URL=
CRON_SCHEDULE=

# STXN Kickoff Vars
CHAIN_ID=""
WS_CHAIN_URL=""
LAMINATOR_ADDRESS=""
CALL_BREAKER_ADDRESS=""
KITN_DISBURSEMENT_SCHEDULER_ADDRESS=""

# Choose the environment
PS3="Please choose the environment: "
options=("local" "dev" "prod" "quit")
select OPT in "${options[@]}"
do
  case ${OPT} in
    "local")
        echo "Using local environment"
        SCHEDULER_HOST="localhost"
        ETH_NETWORK_URL_MAIN="https://sepolia.base.org"
        CONTRACT_ADDRESS_MAIN="0xDc41655b749E8F2922A6E5e525Fc04a915aEaFAA"
        PIPELINES_MAIN_PORT="8090"
        REACT_APP_REF_API_ENDPOINT="http://localhost:8080/write_referral/"
        SOLVER_URL="http://localhost:8888/report"
        CHAIN_ID="21363"
        WS_CHAIN_URL="wss://service.lestnet.org:8888/"
        LAMINATOR_ADDRESS="0x36aB7A6ad656BC19Da2D5Af5b46f3cf3fc47274D"
        CALL_BREAKER_ADDRESS="0x23912387357621473Ff6514a2DC20Df14cd72E7f"
        KITN_DISBURSEMENT_SCHEDULER_ADDRESS="0x7E485Fd55CEdb1C303b2f91DFE7695e72A537399"
        CRON_SCHEDULE="0 */5 * * * *"
        break
        ;;
    "dev")
        echo "Using dev environment"
        SCHEDULER_HOST="dev.api.cleanapp.io"
        ETH_NETWORK_URL_MAIN="https://sepolia.base.org"
        CONTRACT_ADDRESS_MAIN="0xDc41655b749E8F2922A6E5e525Fc04a915aEaFAA"
        PIPELINES_MAIN_PORT="8090"
        REACT_APP_REF_API_ENDPOINT="http://dev.api.cleanapp.io:8080/write_referral/"
        SOLVER_URL="http://104.154.119.169:8888/report"
        CHAIN_ID="21363"
        WS_CHAIN_URL="wss://service.lestnet.org:8888/"
        LAMINATOR_ADDRESS="0x36aB7A6ad656BC19Da2D5Af5b46f3cf3fc47274D"
        CALL_BREAKER_ADDRESS="0x23912387357621473Ff6514a2DC20Df14cd72E7f"
        KITN_DISBURSEMENT_SCHEDULER_ADDRESS="0x7E485Fd55CEdb1C303b2f91DFE7695e72A537399"
        CRON_SCHEDULE="0 */5 * * * *"
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
        SOLVER_URL="http://104.154.119.169:8888/report"
        CHAIN_ID="21363"
        WS_CHAIN_URL="wss://service.lestnet.org:8888/"
        LAMINATOR_ADDRESS="0x36aB7A6ad656BC19Da2D5Af5b46f3cf3fc47274D"
        CALL_BREAKER_ADDRESS="0x23912387357621473Ff6514a2DC20Df14cd72E7f"
        KITN_DISBURSEMENT_SCHEDULER_ADDRESS="0x7E485Fd55CEdb1C303b2f91DFE7695e72A537399"
        CRON_SCHEDULE="0 */5 * * * *"
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

# Cleanapp Web env variables
REACT_APP_PLAYSTORE_URL="https://play.google.com/store/apps/details?id=com.cleanapp"
REACT_APP_APPSTORE_URL="https://apps.apple.com/us/app/cleanapp/id6466403301"

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
      - CRON_SCHEDULE=${CRON_SCHEDULE}

volumes:
  mysql:

COMPOSE

# Install dependencies:
installDocker() {
    # See instructions at https://docs.docker.com/engine/install/ubuntu/
    for pkg in docker.io docker-doc docker-compose docker-compose-v2 podman-docker containerd runc
    do
        sudo apt-get remove $pkg
    done

    # Add Docker's official GPG key:
    sudo apt-get update
    sudo apt-get install ca-certificates curl gnupg
    sudo install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
    sudo chmod a+r /etc/apt/keyrings/docker.gpg

    # Add the repository to Apt sources:
    echo \
    "deb [arch="$(dpkg --print-architecture)" signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
    "$(. /etc/os-release && echo "$VERSION_CODENAME")" stable" | \
    sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
    sudo apt-get update

    # Actually install docker
    sudo apt-get install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

    #Add docker-compose:
    sudo apt  install docker-compose

    # Check that it all works:
    sudo docker run hello-world

    # Configure the current user for docker usage
    sudo chmod a+rw /var/run/docker.sock

    # Configure gcloud fir docker usage
    gcloud auth configure-docker ${DOCKER_LOCATION}
}

# Install docker.
if [[ "$1" == "dockerinstall" ]]; then
  installDocker
fi

set -e

# Pull images:
docker pull ${SERVICE_DOCKER_IMAGE}
docker pull ${PIPELINES_DOCKER_IMAGE}
docker pull ${DB_DOCKER_IMAGE}
docker pull ${WEB_DOCKER_IMAGE}
docker pull ${STXN_KICKOFF_DOCKER_IMAGE}

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
