FROM golang:1.21-alpine3.18 as build

COPY . /code

ENV CGO_ENABLED=0
ENV GOOS=linux

WORKDIR /code

RUN go get -d -v  github.com/go-sql-driver/mysql

RUN go build -a -tags netgo -ldflags '-extldflags "-static"'  -installsuffix cgo -o agent /code/main.go

FROM alpine:3.18

RUN apk add --no-cache curl ca-certificates htop

COPY --from=build /code/agent /usr/local/bin/agent/agent.sh

# Copy the agent to the container
COPY openrc-service.sh /etc/init.d/agent

COPY mktemp /

RUN apk add --no-cache curl

EXPOSE 8080:8080

CMD ["/usr/local/bin/agent/agent.sh"]

