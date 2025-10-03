
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
npm install -g yarn                 # Install yarn globally
yarn                                # Install packages
./manage.sh dev_run_notify_reports  # Run main EPC pusher process
```

### Inspect the DB:

```bash
./manage.sh dbshell cleanapp_dev
```


## Prod / staging workflow

On first run, load the schema in `lib/sql/epc_schema.sql` into the database.

1. Complete the config in .env.production.local (also uncomment vars as appropriate)
1. Build with: `docker build -t epc` or similar
1. Run with `docker run epc` (this runs prod.sh, which exports commands, but you can also run shell commands).

If it complains about a missing environment variable, you can pass it using `docker run -e VAR=val ...`.
