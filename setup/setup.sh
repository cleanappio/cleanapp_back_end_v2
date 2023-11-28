# Full Cleanapp.io setup on a clean Linux machine.
#
# Pre-reqs:
# 1. Linux machine: Debian/Ubuntu/...
# 2. setup.sh file from our setup folder locally in a local folder
#    (pulled from Github or otherwise).
#
# Give any arg to skip the installation, e.g. "./setup.sh local"

# Create necessary files.

cat >.env << ENV
# Setting secrets, please, update with real passwords and save/exit editor.
MYSQL_ROOT_PASSWORD=secret
MYSQL_APP_PASSWORD=secret
MYSQL_READER_PASSWORD=secret
ENV

cat >up.sh << UP
# Turn up CleanApp service.
# Assumes dependencies are in place (docker)
docker-compose up -d
UP
chmod a+x up.sh

cat >down.sh << DOWN
# Turn down CleanApp service.
docker-compose down
# To clean up the database:
# docker-compose down -v
DOWN
chmod a+x down.sh

# Create docker-compose.yml file.
cat >docker-compose.yml << COMPOSE
version: '3'

services:
  cleanappserver:
    container_name: cleanappserver
    image: ibnazer/cleanappserver:1.6
    ports:
      - 8080:8080

  cleanupdb:
    container_name: cleanappdb
    image: ibnazer/cleanappdb:1.6
    environment:
      - MYSQL_ROOT_PASSWORD=\$MYSQL_ROOT_PASSWORD
    volumes:
      - mysql:/var/lib/mysql
    ports:
      - 3306:3306

volumes:
  mysql:

COMPOSE

# Set passwords. On the target machine change to yhour favorite etxt editor:
vim .env

# Docker install
read -p "Do you wish to install this program? [y/N]" yn

if [[ "$yn" != "y" && "$yn" != "Y" ]]
then 
    echo "Not installing. Bye."
    exit
fi

# case $yn in
#     [Yy]* ) echo "Intsalling...";;
#     * ) echo "Not installing. Bye."; exit;;
# esac

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
}

# # Install docker.
# installDocker

# # Pull images:
# docker pull ibnazer/cleanappserver:1.6
# docker pull ibnazer/cleanappdb:1.6

# # Start our docker images.
# ./up.sh

echo "*** We are running, done."
