FROM --platform=linux/amd64 mysql:8.0
# RUN DEBIAN_FRONTEND=noninteractive \
#   apt-get sed
# RUN yum install sed
# RUN apt-get sed
# RUN echo "" | sed "s|a|b|"  

USER root
COPY ./setup.sql /docker-entrypoint-initdb.d/setup.sql
COPY ./init.sql .
COPY ./set_passwords.sh .
COPY ./patches /docker-entrypoint-initdb.d/patches

# RUN useradd -ms /bin/bash cleanapper
# USER cleanapper

EXPOSE 3306/tcp
# CMD [ "./set_passwords.sh" ]
#ENTRYPOINT [ "./set_passwords.sh" ]
#CMD ["mysqld"]
