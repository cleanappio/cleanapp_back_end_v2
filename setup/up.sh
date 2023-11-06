# Turn up Cleanapp service.
# Assumes dependencies are in place (docker)

# Setting secrets, update before running.
MYSQL_ROOT_PASSWORD=secret
MYSQL_APP_PASSWORD=secret
MYSQL_READER_PASSWORD=secret

docker-compose up -d
