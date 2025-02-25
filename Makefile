# Define the binary name and output directory
BINARY_NAME=prometheus_s3_exporter
OUTPUT_DIR=bin

# Define the main package
MAIN_PACKAGE=main.go

# go version prints, i.e.: go version go1.18.5 darwin/arm64
# so we can use that as an os-independent way to figure out our current os and arch
os_arch = $(word 4, $(shell go version))
os = $(word 1,$(subst /, ,$(os_arch)))
arch = $(word 2,$(subst /, ,$(os_arch)))

# Default target
all: build

# Build the binary
build:
	@echo "Building the binary for ${os}/${arch}"
	@mkdir -p $(OUTPUT_DIR)
	GOOS=${os} GOARCH=${arch} go build -o $(OUTPUT_DIR)/$(BINARY_NAME) $(MAIN_PACKAGE)
	@chmod +x $(OUTPUT_DIR)/$(BINARY_NAME)
	@echo "Binary built and placed in $(OUTPUT_DIR)/$(BINARY_NAME)"

# Clean the output directory
clean:
	@echo "Cleaning the output directory..."
	rm -rf $(OUTPUT_DIR)
	@echo "Output directory cleaned"

# Run the binary
run: build
	@echo "Running the binary..."
	$(OUTPUT_DIR)/$(BINARY_NAME)

# Help target
help:
	@echo "Makefile targets:"
	@echo "  all     - Build the binary (default target)"
	@echo "  build   - Build the binary"
	@echo "  clean   - Clean the output directory"
	@echo "  run     - Run the binary"
	@echo "  help    - Show this help message"

.PHONY: all build clean run help