# Stage 1: Build the test server application
FROM golang:1.23-alpine AS build

# Set the working directory
WORKDIR /app

# Set the Go proxy
ENV GOPROXY=https://goproxy.io,https://goproxy.cn

# Copy go.mod and go.sum for dependency management if needed
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the source code for the test server application
COPY ./test_server_app ./test_server_app

# Build the test server application
RUN go build -v -o app ./test_server_app/app.go

# Check the files created in the build stage (debugging line)
RUN ls -l /app  # Check if the test_server_app executable is present

# Stage 2: Create a minimal runtime image
FROM alpine:latest

# Set the working directory
WORKDIR /app

# Copy the compiled test server application from the build stage
COPY --from=build /app/app .

# Check the files in the runtime stage (debugging line)
RUN ls -l /app  # Check if the executable is present in the final image

# Ensure the executable has permissions to run
RUN chmod +x ./app  # Ensure the app binary is executable

# Expose the port for the test server if needed
EXPOSE 5000

# Command to run the test server application
CMD ["./app"]
