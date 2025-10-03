
FROM node:22-alpine

WORKDIR /app

COPY . .

RUN apk add jq git bash \
  && npm config set update-notifier false \
  && yarn

ENTRYPOINT ["/app/prod.sh"]
