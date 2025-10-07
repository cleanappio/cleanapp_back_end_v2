
# Backend processes for Ethereum Place Codes

## Solution Overview

You want an on-chain address for each Ethereum Place Code (EPC), potentially with some smart contract functions down the line, and you dont want to administrate a private key for each EPC. This presents a problem, since the address of EPC contract creation depends either on a nonce of the creating address (the order of which will be unknown, making it impossible to predict the address) or the hash of the contract code (which you want to be able to upgrade, again making it impossible to predict); so, there is a method called "CREATE3" which uses some trickery to get a predictable contract address using only a pre-determined key, which you can then mint if/whenever you need to.

The process uses an associated smart contract factory ([link](https://github.com/ssadler/epc-contracts)) which can be upgraded to mint the EPC contracts at such a time as we know what they will be, but in the meantime, it is able to return an EPC address given a key. The EPC key is stored in the epc_contracts table. The message is associated with a campaign so you can track sent messages and why you sent them.

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
