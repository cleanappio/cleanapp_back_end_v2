FROM us-central1-docker.pkg.dev/cleanup-mysql-v2/cleanapp-docker-repo/cleanapp-db-image:live

# This image wraps the existing DB image so we can distinguish a replica
# deployment in compose and evolve replica-specific defaults over time.
# All replication/clone configuration is provided via container command
# options and SQL executed by setup scripts, not baked into the image.

EXPOSE 3306/tcp

