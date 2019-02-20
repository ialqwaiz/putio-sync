FROM golang:latest

WORKDIR /go/src/app
COPY . .

RUN go get -u github.com/ialqwaiz/putio-sync

EXPOSE 3000
CMD ["putio-sync","-server"]
