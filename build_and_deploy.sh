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

for project_dir in areas-service auth-service brand-dashboard custom-area-dashboard customer-service report-analyze-pipeline report-listener report-processor email-service report-ownership-service; do
    pushd $project_dir
    if [ -f "build_image.sh" ]; then
        echo "Building image for ${project_dir}..."
        ./build_image.sh -e ${OPT}
    elif [ -f "build_images.sh" ]; then
        echo "Building images for ${project_dir}..."
        ./build_images.sh -e ${OPT}
    else
        echo "No build_image.sh or build_images.sh found for ${project_dir}, skipping..."
    fi
    popd
done

pushd setup
./setup_from_local.sh -e ${OPT} --ssh-keyfile ${SSH_KEYFILE}
popd
