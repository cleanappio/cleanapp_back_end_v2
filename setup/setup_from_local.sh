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

case ${OPT} in
  "dev")
      echo "Using dev environment"
      TARGET_VM_IP="34.132.121.53"
      ;;
  "prod")
      echo "Using prod environment"
      TARGET_VM_IP="34.122.15.16"
      ;;
  *)
    echo "Usage: $0 -e|--env <dev|prod> [--ssh-keyfile <ssh_keyfile>]"
    exit 1
    ;;
esac

if [ -n "${SSH_KEYFILE}" ]; then
  SETUP_SCRIPT="https://raw.githubusercontent.com/cleanappio/cleanapp_back_end_v2/refs/heads/main/setup/setup.sh"
  
  # Copy deployment script on target VM and run it 
  curl ${SETUP_SCRIPT} | ssh -i ${SSH_KEYFILE} deployer@${TARGET_VM_IP} "cat > deploy.sh && chmod +x deploy.sh && ./deploy.sh -e ${OPT}"
fi
