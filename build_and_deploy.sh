OPT=""
SSH_KEYFILE=""
while [[ $# -gt 0 ]]; do
  case $1 in
    "-e"|"--env")
      OPT="$2"
      shift 2
      ;;
    "--ssh-keyfile")
      SSH_KEYFILE="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

if [ -z "${OPT}" ]; then
  echo "Usage: $0 -e|--env <dev|prod> [--ssh-keyfile <ssh_keyfile>]"
  exit 1
fi

if [ -z "${SSH_KEYFILE}" ]; then
  echo "Usage: $0 -e|--env <dev|prod> [--ssh-keyfile <ssh_keyfile>]"
  exit 1
fi

for project_dir in auth-service brand-dashboard customer-service montenegro-areas report-analyze-pipeline report-listener; do
    pushd $project_dir
    ./build_image.sh -e ${OPT}
    popd
done

pushd setup
./setup_from_local.sh -e ${OPT} --ssh-keyfile ${SSH_KEYFILE}
popd
