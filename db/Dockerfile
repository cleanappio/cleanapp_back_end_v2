FROM --platform=linux/amd64 mysql:8.0

#RUN echo 'APT::Install-Suggests "0";' >> /etc/apt/apt.conf.d/00-docker
#RUN echo 'APT::Install-Recommends "0";' >> /etc/apt/apt.conf.d/00-docker
#RUN DEBIAN_FRONTEND=noninteractive \
#   apt-get update \
#   && rm -rf /var/lib/apt/lists/*
 
USER root
COPY ./setup.sql /docker-entrypoint-initdb.d/setup.sql

# RUN useradd -ms /bin/bash cleanapper
# USER cleanapper

EXPOSE 3306/tcp
# CMD [ "./service" ]
# ENTRYPOINT [ "./service" ]

