APP_NAME=web-scrapper
BUILD_DIR=bin

.PHONY: build run test clean vet fmt

build:
	go build -o $(BUILD_DIR)/$(APP_NAME) ./cmd/server

run: build
	./$(BUILD_DIR)/$(APP_NAME)

test:
	go test ./... -v

vet:
	go vet ./...

fmt:
	go fmt ./...

clean:
	rm -rf $(BUILD_DIR)
