FROM golang:1.24-alpine AS builder

WORKDIR /app
RUN apk add --no-cache git ca-certificates

COPY go-common /go-common
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN if [ -f buildinfo.vars ]; then . ./buildinfo.vars; else CLEANAPP_BUILD_VERSION=dev; CLEANAPP_GIT_SHA=; CLEANAPP_BUILD_TIME=; fi && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w -X report-analysis-backfill/version.BuildVersion=${CLEANAPP_BUILD_VERSION} -X report-analysis-backfill/version.GitSHA=${CLEANAPP_GIT_SHA} -X report-analysis-backfill/version.BuildTime=${CLEANAPP_BUILD_TIME}" -a -installsuffix cgo -o report-analysis-backfill .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/report-analysis-backfill .
EXPOSE 8080
CMD ["./report-analysis-backfill"]
