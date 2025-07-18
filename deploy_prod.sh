for project_dir in auth-service brand-dashboard customer-service montenegro-areas report-analyze-pipeline report-listener; do
    pushd $project_dir
    ./build_image.sh -e prod
    popd
done