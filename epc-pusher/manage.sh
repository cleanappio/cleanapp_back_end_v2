#!/bin/bash
# vim: set noai ts=4 sw=4

# add to your .bashrc:
# alias m='./manage.sh'

set -e

DB_CONTAINER=cleanapp_dev_mysql

function dev_deploy () {

  docker kill $DB_CONTAINER || true && docker rm $DB_CONTAINER || true
  docker run --name $DB_CONTAINER -p3406:3306 -e MYSQL_ROOT_PASSWORD="$DB_PASSWORD" -d mysql

  sleep 15 # db startup takes a long time because im too lazy to make volume persistent

  echo "Creating database..."
  echo "create database $DB_NAME" | dbshell

  echo "Loading DB dump..."
  cat dev_dump_2025-08-28.sql | dbshell $DB_NAME

  echo "Loading schema"
  dev_reset_schema

  echo "All done!"
}

function dev_reset_schema () {
  # Just recreate epc tables
  cat lib/sql/epc_schema.sql | \
    sed -nE -e 's/create table if not exists ([^ ]+).*/drop table if exists \1;/ip' | \
    tac | dbshell $DB_NAME

  cat lib/sql/epc_schema.sql | dbshell $DB_NAME
}

function dbshell () {
  MYSQL_PWD="$DB_PASSWORD" mysql -h 127.0.0.1 -P 3406 -u root $@
}

function dev_run_notify_reports () {
  npx tsx src/notifyReports.ts
}

function import_contracts () {
  dir="${1:-../../epc-contracts/out}"
  rm -rf lib/abi
  mkdir -p lib/abi

  function f () {
    name=`basename ${1%.json}`
    out=lib/abi/`basename $1`
    cat $1 | jq ". | {bytecode: .bytecode.object, abi, name: \"$name\"}" > $out
  }

  f "$dir/Pure.sol/EPCPure.json"
}



if [ -z "$1" ]; then
  echo "Commands:"
  echo
  cat $0 | sed -rne 's/^function ([^ \(]+).*/  \1/p'
  echo
else

  export NODE_ENV="development"
  touch .env.development.local

  set -a # auto export variables
  . .env
  . .env.development
  . .env.development.local
  set +a # end auto export


  cmd=$1           # Get the function name from argv
  shift            # Remove function name
  eval $cmd $@     # Call function and parse arguments
fi
