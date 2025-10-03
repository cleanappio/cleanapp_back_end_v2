#!/bin/bash

set -e

function notify_reports () {
  npx tsx src/notifyReports.ts
}


if [ -z "$1" ]; then
  echo "Commands:"
  echo
  cat $0 | sed -rne 's/^function ([^ \(]+).*/  \1/p'
  echo
else

  . lib/common.sh

  export NODE_ENV="production"

  set -a # auto export variables
  . .env
  . .env.production
  . .env.production.local
  set +a # end auto export


  cmd=$1           # Get the function name from argv
  shift            # Remove function name
  eval $cmd $@     # Call function and parse arguments
fi
