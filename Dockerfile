FROM golang:1.26.3 AS builder

WORKDIR /go/src/app

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o DPAppRegistry .
RUN CGO_ENABLED=0 GOOS=linux go test -c -o DPAppRegistry_tests

FROM golang:1.26.3-alpine3.22

RUN addgroup -S dpappregistry && adduser -S -G dpappregistry dpappregistry

WORKDIR /app

COPY --from=builder /go/src/app/LICENSE /app/LICENSE
COPY --from=builder /go/src/app/mongod/migrations /app/mongod/migrations
COPY --from=builder /go/src/app/DPAppRegistry /usr/bin
COPY --from=builder /go/src/app/DPAppRegistry_tests /usr/bin

RUN chown -R dpappregistry:dpappregistry /app /usr/bin/DPAppRegistry /usr/bin/DPAppRegistry_tests

USER dpappregistry

CMD ["DPAppRegistry"]
