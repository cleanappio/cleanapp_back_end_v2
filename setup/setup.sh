# Full Cleanapp.io setup on a clean Linux machine.
#
# Pre-reqs:
# 1. Linux machine: Debian/Ubuntu/...
# 2. setup.sh file from our setup folder locally in a local folder
#    (pulled from Github or otherwise).

# Vars init
SCHEDULER_HOST=""
ETH_NETWORK_URL=""
CONTRACT_ADDRESS=""

# Choose the environment
PS3="Please choose the environment: "
options=("local" "dev" "prod" "quit")
select OPT in "${options[@]}"
do
  case ${OPT} in
    "local")
        echo "Using local environment"
        SCHEDULER_HOST="localhost"
        ETH_NETWORK_URL="https://sepolia.base.org"
        CONTRACT_ADDRESS="0xDc41655b749E8F2922A6E5e525Fc04a915aEaFAA"
        break
        ;;
    "dev")
        echo "Using dev environment"
        SCHEDULER_HOST="dev.api.cleanapp.io"
        ETH_NETWORK_URL="https://sepolia.base.org"
        CONTRACT_ADDRESS="0xDc41655b749E8F2922A6E5e525Fc04a915aEaFAA"
        break
        ;;
    "prod")
        echo "Using prod environment"
        SCHEDULER_HOST="api.cleanapp.io"
        ETH_NETWORK_URL="https://sepolia.base.org"  # TODO: Change to the mainnet URL after we run on the base mainnet
        CONTRACT_ADDRESS="0xDc41655b749E8F2922A6E5e525Fc04a915aEaFAA"  # TODO: Change the contract address to the main when we run on the base mainnet
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
KITN_PRIVATE_KEY=\$(gcloud secrets versions access 1 --secret="KITN_PRIVATE_KEY_${SECRET_SUFFIX}")

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

# Cleanapp Web env variables
REACT_APP_REF_API_ENDPOINT="http://dev.api.cleanapp.io:8080/write_referral/"
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
    ports:
      - 8080:8080

  cleanapp_pipelines:
    container_name: cleanapp_pipelines
    image: ${PIPELINES_DOCKER_IMAGE}
    environment:
      - MYSQL_ROOT_PASSWORD=\${MYSQL_ROOT_PASSWORD}
      - MYSQL_APP_PASSWORD=\${MYSQL_APP_PASSWORD}
      - KITN_PRIVATE_KEY=\${KITN_PRIVATE_KEY}
      - ETH_NETWORK_URL=${ETH_NETWORK_URL}
      - CONTRACT_ADDRESS=${CONTRACT_ADDRESS}
    ports:
      - 8090:8090

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

# Start our docker images.
./up.sh

# Skip scheduling for a local environment.
if [[ "${OPT}" == "local" ]]; then
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
  --uri="http://${SCHEDULER_HOST}:8090/referrals_redeem" \
  --message-body="{\"version\": \"2.0\"}" \
  --headers="Content-Type=application/json"

# Tokens disbursement schedule
DISBURSEMENT_SCHEDULER_NAME="tokens-disburse-${OPT}"
EXISTING_DISBURSEMENT_SCHEDULER=$(gcloud scheduler jobs list --location=us-central1 | grep ${DISBURSEMENT_SCHEDULER_NAME} | awk '{print $1}')

if [[ "${DISBURSEMENT_SCHEDULER_NAME}" == "${EXISTING_DISBURSEMENT_SCHEDULER}" ]]; then
  gcloud scheduler jobs delete ${DISBURSEMENT_SCHEDULER_NAME} \
    --location=us-central1 \
    --quiet
fi

gcloud scheduler jobs create http ${DISBURSEMENT_SCHEDULER_NAME} \
  --location=us-central1 \
  --schedule="0 20 * * *" \
  --uri="http://${SCHEDULER_HOST}:8090/tokens_disburse" \
  --message-body="{\"version\": \"2.0\"}" \
  --headers="Content-Type=application/json"
