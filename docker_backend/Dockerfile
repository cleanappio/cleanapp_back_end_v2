FROM --platform=linux/amd64 ubuntu:22.04
RUN echo 'APT::Install-Suggests "0";' >> /etc/apt/apt.conf.d/00-docker
RUN echo 'APT::Install-Recommends "0";' >> /etc/apt/apt.conf.d/00-docker
RUN DEBIAN_FRONTEND=noninteractive \
   apt-get update \
   && rm -rf /var/lib/apt/lists/*
 
USER root

RUN apt-get update
RUN apt-get install -y ca-certificates

COPY ./service .

# Copy certificates to connect to the ethereum network
COPY certificates/* /usr/local/share/ca-certificates/
RUN update-ca-certificates

# RUN useradd -ms /bin/bash cleanapper
# USER cleanapper

EXPOSE 8080/tcp
CMD "./service" \
   "--mysql_password=${MYSQL_APP_PASSWORD}" \
   "--mysql_host=cleanapp_db" \
   "--eth_network_url_main=${ETH_NETWORK_URL_MAIN}" \
   "--eth_private_key_main=${KITN_PRIVATE_KEY_MAIN}" \
   "--contract_address_main=${CONTRACT_ADDRESS_MAIN}" \
   "--solver_url=${SOLVER_URL}" \
   "--sendgrid_api_key=${SENDGRID_API_KEY}" \
   "--sendgrid_from_name=${SENDGRID_FROM_NAME}" \
   "--sendgrid_from_email=${SENDGRID_FROM_EMAIL}"
