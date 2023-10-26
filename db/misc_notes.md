# Notes on running mysql in Docker

A bit chaotic at the moment.

Official mysql image on DockerHub: https://hub.docker.com/_/mysql (also has some instructions)
Labels are versions: use 8.0

Pulling image: 
```shell
docker pull mysql:8.0
```

Mount to /var/lib/mysql within the container.

MYSQL_ROOT_PASSWORD environment variable sets the root password.

Run manually:
```shell
docker run --name my-mysql -e MYSQL_ROOT_PASSWORD=secret -v $HOME/mysql-data:/var/lib/mysql -d mysql:8.0
```

## docker-compose.yml file to set the params:

```yml
version: "3"

services:
  mysql:
    image: mysql:8.0
    environment:
      - MYSQL_ROOT_PASSWORD
    volumes:
      - mysql:/var/lib/mysql

volumes:
  mysql:
```

Starting with docker-compose.yml:

```shell
MYSQL_ROOT_PASSWORD=secret docker-compose up -d
```

## Connecting to mysql from host:

```shell
docker exec -it my-mysql mysql -p
```

Get shell:
```shell
docker exec -it my-mysql bash
```

Get logs:
```shell
docker logs my-mysql
```

### Using custom config:

If your custom config file is /my/custom/config-file.cnf run
```shell
docker run --name some-mysql -v /my/custom:/etc/mysql/conf.d -e MYSQL_ROOT_PASSWORD=my-secret-pw -d mysql:tag
```

Notice that file name is not used, it's fixed: ```config-file.cnf``` 

### Creating database dumps
```shell
docker exec some-mysql sh -c 'exec mysqldump --all-databases -uroot -p"$MYSQL_ROOT_PASSWORD"' > /some/path/on/your/host/all-databases.sql
```

### Restoring data from dump files
```shell
docker exec -i some-mysql sh -c 'exec mysql -uroot -p"$MYSQL_ROOT_PASSWORD"' < /some/path/on/your/host/all-databases.sql
```

### Copy file from host to container
```shell
docker cp /path/of/the/file <Container_ID>:/path/of/he/container/folder
```

### Specify host path to bind

```shell
version: '2'
services:
  db:
    image: mysql
    volumes:
      - dbdata:/var/lib/mysql   # <<<--- container path
volumes:
  dbdata:
    driver: local
    driver_opts:
      type: 'none'
      o: 'bind'
      device: '/srv/db-data'  # <<<--- host path
``` 



## Notable links

[1] https://hub.docker.com/_/mysql

[2] https://docs.docker.com/storage/volumes/#mount-a-host-directory-as-a-data-volume

[3] https://www.howtogeek.com/devops/how-to-run-mysql-in-a-docker-container/
