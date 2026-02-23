# Makefile HOROS 47 SINGULARITY
# Pure Go build (CGO désactivé, ONNX Runtime supprimé)

# Chemins
BIN_DIR=bin
CMD_MASTER=cmd/horos47
CMD_VISION_SLAVE=cmd/horos_vision
CMD_INDEXER=cmd/embedding_indexer
CMD_GPU_FEEDER=cmd/gpu_feeder_v3
BINARY_MASTER=$(BIN_DIR)/horos47
BINARY_VISION_SLAVE=$(BIN_DIR)/horos_vision
BINARY_INDEXER=$(BIN_DIR)/embedding_indexer
BINARY_GPU_FEEDER=$(BIN_DIR)/horos_gpu_feeder

# Cibles
.PHONY: all build build-master build-vision-slave build-indexer build-gpu-feeder clean run test

all: build

build: build-master build-vision-slave build-indexer build-gpu-feeder

build-master:
	@echo "Building HOROS 47 Master (Pure Go, CGO_ENABLED=0)..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -v -o $(BINARY_MASTER) ./$(CMD_MASTER)/
	@echo "✓ Master build completed: $(BINARY_MASTER)"

build-vision-slave:
	@echo "Building HOROS Vision Slave (Pure Go, HTTP client to vLLM)..."
	@mkdir -p $(BIN_DIR)
	cd $(CMD_VISION_SLAVE) && \
		CGO_ENABLED=0 \
		go build -v -o ../../$(BINARY_VISION_SLAVE) .
	@echo "✓ Vision Slave build completed: $(BINARY_VISION_SLAVE)"

build-indexer:
	@echo "Building Embedding Indexer Worker (Pure Go)..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -v -o $(BINARY_INDEXER) ./$(CMD_INDEXER)/
	@echo "✓ Indexer build completed: $(BINARY_INDEXER)"

build-gpu-feeder:
	@echo "Building GPU Feeder Orchestrator (Pure Go)..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -v -o $(BINARY_GPU_FEEDER) ./$(CMD_GPU_FEEDER)/
	@echo "✓ GPU Feeder build completed: $(BINARY_GPU_FEEDER)"

clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BIN_DIR)
	@echo "✓ Clean completed"

run: build
	@echo "Starting HOROS 47 SINGULARITY..."
	./$(BINARY)

test:
	@echo "Running tests (Pure Go)..."
	go test -v ./...

# Installation dépendances Go
deps:
	@echo "Installing Go dependencies..."
	go mod download
	go mod verify
	@echo "✓ Dependencies installed"
