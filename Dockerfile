FROM golang:1.26.3 AS builder

WORKDIR /go/src/app

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o DPAppRegistry .
RUN CGO_ENABLED=0 GOOS=linux go test -c -o DPAppRegistry_tests

FROM golang:1.26.3-alpine3.22

# A fixed uid and gid, so the log files in the app_logs volume keep one owner
# from one image to the next.
RUN addgroup -S -g 10001 dpappregistry && adduser -S -u 10001 -G dpappregistry dpappregistry

WORKDIR /app

COPY --from=builder /go/src/app/LICENSE /app/LICENSE
COPY --from=builder /go/src/app/mongod/migrations /app/mongod/migrations
COPY --from=builder /go/src/app/DPAppRegistry /usr/bin
COPY --from=builder /go/src/app/DPAppRegistry_tests /usr/bin

# /app/logs is LOG_DIR in production; a new volume mounted there takes its owner.
RUN mkdir -p /app/logs && chown -R dpappregistry:dpappregistry /app /usr/bin/DPAppRegistry /usr/bin/DPAppRegistry_tests

USER dpappregistry

CMD ["DPAppRegistry"]
