# Full Cleanapp.io setup on a clean Linux machine.
#
# Pre-reqs:
# 1. Linux machine: Debian/Ubuntu/...
# 2. setup.sh file from our setup folder locally in a local folder
#    (pulled from Github or otherwise).
#
# Give any arg to skip the installation, e.g. "./setup.sh local"

# Docker images label:
DOCKER_LABEL='1.6'

# Create necessary files.

cat >.env << ENV
# Setting secrets, please, edit this file
# to set the real passwords and then save
# and exit the editor to let the script contiueß.
MYSQL_ROOT_PASSWORD=secret
MYSQL_APP_PASSWORD=secret
MYSQL_READER_PASSWORD=secret
ENV

cat >up.sh << UP
# Turn up CleanApp service.
# Assumes dependencies are in place (docker)
sudo docker-compose up -d --remove-orphans
UP
sudo chmod a+x up.sh

cat >down.sh << DOWN
# Turn down CleanApp service.
sudo docker-compose down
# To clean up the database:
# sudo docker-compose down -v
DOWN
sudo chmod a+x down.sh

# Create docker-compose.yml file.
cat >docker-compose.yml << COMPOSE
version: '3'

services:
  cleanappserver:
    container_name: cleanappserver
    image: ibnazer/cleanappserver:1.6
    environment:
      - MYSQL_ROOT_PASSWORD=\$MYSQL_ROOT_PASSWORD
      - MYSQL_APP_PASSWORD=\$MYSQL_APP_PASSWORD
    ports:
      - 8080:8080

  cleanappdb:
    container_name: cleanappdb
    image: ibnazer/cleanappdb:1.6
    environment:
      - MYSQL_ROOT_PASSWORD=\$MYSQL_ROOT_PASSWORD
      - MYSQL_APP_PASSWORD=\$MYSQL_APP_PASSWORD
      - MYSQL_READER_PASSWORD=\$MYSQL_READER_PASSWORD
    volumes:
      - mysql:/var/lib/mysql
    ports:
      - 3306:3306

  cleanappapp:
    container_name: cleanappapp
    image: ibnazer/cleanappapp:1.6
    ports:
      - 3000:3000

volumes:
  mysql:

COMPOSE

# Set passwords. On the target machine you can change 'vim' to your favorite text editor:
vim .env

# Docker install
read -p "Do you wish to install this program? [y/N]" yn

if [[ "$yn" != "y" && "$yn" != "Y" ]]
then 
    echo "Not installing. Bye."
    exit
fi

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

# Install docker.
installDocker

# Pull images:
sudo docker pull ibnazer/cleanappserver:${DOCKER_LABEL}
sudo docker pull ibnazer/cleanappdb:${DOCKER_LABEL}
sudo docker pull ibnazer/cleanappapp:${DOCKER_LABEL}

# Start our docker images.
./up.sh

# Cleanup passwords from the disk.
sudo rm .env

echo "*** We are running, done."
