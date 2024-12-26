# Coding Challenge

## Run the Application with Docker

1. **Build the Docker image**  
   Use the following command to build the Docker image for the service:
   ```bash
   sudo docker build -t ev-pooling-service -f _ci/build/coding-challenge/Dockerfile
   ```
2. **Run the Docker container**  
   Start the application by running the container:
   ```bash
   sudo docker run -p 80:80 ev-pooling-service
   ```

## Run Tests

1. **Initialize the Go module**  
   go mod init gitlab.com/me141952/coding-challenge

2. **Install dependencies**  
   go mod tidy

3. **Run the tests**  
   go test -v
