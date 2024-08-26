FROM --platform=linux/amd64 ubuntu:22.04
RUN echo 'APT::Install-Suggests "0";' >> /etc/apt/apt.conf.d/00-docker
RUN echo 'APT::Install-Recommends "0";' >> /etc/apt/apt.conf.d/00-docker
RUN DEBIAN_FRONTEND=noninteractive \
   apt-get update \
   && rm -rf /var/lib/apt/lists/*
 
USER root

RUN apt-get update
RUN apt-get install -y ca-certificates

COPY ./pipelines .

# Copy certificates to connect to the ethereum network
COPY certificates/* /usr/local/share/ca-certificates/
RUN update-ca-certificates

# RUN useradd -ms /bin/bash cleanapper
# USER cleanapper

EXPOSE 8090/tcp
EXPOSE 8091/tcp
CMD "./pipelines" "--mysql_password=${MYSQL_APP_PASSWORD}" "--mysql_host=cleanapp_db" "--eth_network_url=${ETH_NETWORK_URL}" "--eth_private_key=${KITN_PRIVATE_KEY}" "--contract_address=${CONTRACT_ADDRESS}" "--users_table=${USERS_TABLE}"
