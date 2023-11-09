# Full Cleanapp.io setup on a clean Linux machine.
# Pre-reqs:
# 1. Linux machine: Debian/Ubuntu/...
# 2. Files from our setup folder locally in a local folder
#    (pulled from Github or otherwise).
#    *MUST HAVE: docker-compose.yml* and must update sercrets in .env file.
# 3. Update up.sh with real passwords before the first run!

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
docker pull mysql:8.0
docker pull ibnazer/cleanappserver

# Start our docker images.
./up.sh

# Done, we are running.
echo *** Done, we are running.