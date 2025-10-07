
FROM node:22-alpine

WORKDIR /app

# Install runtime deps
RUN apk add --no-cache bash git jq

# Copy only package manifests first for better layer caching
COPY package.json yarn.lock ./

# Install deps
RUN npm config set update-notifier false \
  && yarn --frozen-lockfile

# Copy source
COPY . .

# Default command runs the notifier in production mode
ENV NODE_ENV=production

ENTRYPOINT ["/app/prod.sh", "notify_reports"]
