# Full Cleanapp.io setup on a clean Linux machine.
# Pre-reqs:
# 1. Linux machine: Debian/Ubuntu/...
# 2. Files from our setup folder locally in a local folder
#    (pulled from Github or otherwise).
#    *MUST HAVE: docker-compose.yml*
# 3. Update up.sh with real passwords before the first run!

# Install docker.
# TODO: Not done yet.

# Pull images:
docker pull mysql:8.0
docker pull ibnazer/cleanapp

# Start our docker images.
./up.sh

# Done, we are running.
echo *** Done, we are running.