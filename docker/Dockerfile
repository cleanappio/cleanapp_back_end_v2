FROM --platform=linux/amd64 ubuntu:22.04
RUN echo 'APT::Install-Suggests "0";' >> /etc/apt/apt.conf.d/00-docker
RUN echo 'APT::Install-Recommends "0";' >> /etc/apt/apt.conf.d/00-docker
RUN DEBIAN_FRONTEND=noninteractive \
   apt-get update \
   && rm -rf /var/lib/apt/lists/*
 
USER root
COPY ./service .

# RUN useradd -ms /bin/bash cleanapper
# USER cleanapper

EXPOSE 8080/tcp
CMD "./service" "--mysql_password=${MYSQL_APP_PASSWORD}" "--mysql_host=cleanappdb" # shell form to get env substitution.
