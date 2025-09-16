
# Backend processes for Ethereum Place Codes

## Dev workflow

### Deploy dev db:

```bash

docker pull mysql
./manage.sh dev_deploy

```

### Test the service:

* Deploy the database as above
* Install node + npm, verson >= v22.15.0 (perhaps using [https://github.com/nvm-sh/nvm](Node Version Manager))
* Run the below to setup yarn, install project packages and run the 

```bash
npm install -g yarn    # Install yarn globally
yarn                   # Install packages
./manage.sh dev_index     # Run main EPC pusher process
```

### Inspect the DB:

```bash
./manage.sh dbshell cleanapp_dev
```

