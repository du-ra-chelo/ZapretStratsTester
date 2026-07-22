.PHONY: build run test clean

BUILD_DIR := ./bin
SERVICES  := main tester

build: $(addprefix build-,$(SERVICES))

build-%:
	go build -o $(BUILD_DIR)/$* ./cmd/$*

run:
	@if [ -z "$(SVC)" ]; then \
		echo "Укажи сервис: make run SVC=api"; \
	else \
		go run ./cmd/$(SVC); \
	fi

test:
	go test ./... -v

clean:
	rm -rf $(BUILD_DIR)
