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

## Replication notes

### To give access to xtrabackup initial snapshot over ssh:
```
# From the host with gcloud secrets update permission:
$ ./setup/xtrabackup_gen_key_and_secret.sh -e dev
$ ./setup/xtrabackup_gen_key_and_secret.sh -e prod
# With the pubkey from the outputs on the replication target host:
deployer@cleanapp-prod2:~/.ssh$ mcedit /home/deployer/.ssh/authorized_keys
# Add the following lines:
from="10.128.0.9",command="docker run -i --rm --network host --mount source=eko_mysql_replica_dev,target=/var/lib/mysql -u 0:0 percona/percona-xtrabackup:8.0 sh -lc 'mkdir -p /var/lib/mysql/seed && cd /var/lib/mysql/seed && xbstream -x'",no-pty,no-agent-forwarding,no-port-forwarding,no-X11-forwarding ssh-ed25519 <DEV_PUBKEY> xtrabackup-dev
from="10.128.0.6",command="docker run -i --rm --network host --mount source=eko_mysql_replica_prod,target=/var/lib/mysql -u 0:0 percona/percona-xtrabackup:8.0 sh -lc 'mkdir -p /var/lib/mysql/seed && cd /var/lib/mysql/seed && xbstream -x'",no-pty,no-agent-forwarding,no-port-forwarding,no-X11-forwarding ssh-ed25519 <PROD_PUBKEY> xtrabackup-prod
```
### Replication flow (prod->prod2 in this example).
1. Wipe destination:
```
bash -lc "ssh -o StrictHostKeyChecking=no deployer@35.238.248.151 'docker stop cleanapp_db || true; docker rm -f cleanapp_db || true; docker volume rm -f eko_mysql_replica_prod || true; docker volume create eko_mysql_replica_prod >/dev/null && echo volume_ready'"
```
2. Snapshot streaming:
```
bash -lc "bash /home/renard/src/github/stxn/cleanapp_back_end_v2/setup/setup-replica.sh -s prod --mode xtrabackup > /home/renard/src/github/stxn/.cursor/.agent-tools/xtrabackup-seed-prod-$(date +%Y%m%d-%H%M%S).log 2>&1 & disown; echo started_clean"
```
3. Check replication:
```
deployer@cleanapp-prod2:~$ docker exec -i cleanapp_db mysql -uroot -p"$MYSQL_ROOT_PASSWORD_PROD" -e "SHOW REPLICA STATUS\\G" | egrep 'Last_IO_Errno|Last_IO_Error|Source_Host|Running:|Seconds_Behind'
                  Source_Host: 10.128.0.6
           Replica_IO_Running: Yes
          Replica_SQL_Running: Yes
        Seconds_Behind_Source: 0
                Last_IO_Errno: 0
                Last_IO_Error: 
      Last_IO_Error_Timestamp: 
```

## Notable links

[1] https://hub.docker.com/_/mysql

[2] https://docs.docker.com/storage/volumes/#mount-a-host-directory-as-a-data-volume

[3] https://www.howtogeek.com/devops/how-to-run-mysql-in-a-docker-container/
