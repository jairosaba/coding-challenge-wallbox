FROM golang:1.20

WORKDIR /app

COPY main.go .

RUN go mod init ev-pooling && \
    go mod tidy && \
    go build -o main main.go

EXPOSE 80

CMD ["./main"]
