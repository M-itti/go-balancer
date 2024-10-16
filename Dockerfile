FROM golang:1.23-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY ./test_server_app ./test_server_app

RUN go build -v -o app ./test_server_app/app.go

RUN ls -l /app  # Check if the test_server_app executable is present

FROM alpine:latest

WORKDIR /app

COPY --from=build /app/app .

RUN ls -l /app  # Check if the executable is present in the final image

RUN chmod +x ./app  

EXPOSE 5000

CMD ["./app"]
