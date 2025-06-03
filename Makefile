SOURCES :=  $(shell find . -name '*.go')
BINARY_NAME := amcrest-go

.PHONY: all clean

all: $(BINARY_NAME)

dist: linux-arm64 linux-amd64 macos-amd64 macos-arm64

$(BINARY_NAME): $(SOURCES)
	go build -o dist/$(BINARY_NAME)

linux-arm64: $(SOURCES)
	GOOS=linux GOARCH=arm64 go build -trimpath -o dist/$(BINARY_NAME)-arm64

linux-amd64: $(SOURCES)
	GOOS=linux GOARCH=amd64 go build -trimpath -o dist/$(BINARY_NAME)-amd64

macos-amd64: $(SOURCES)
	GOOS=darwin GOARCH=amd64 go build -trimpath -o dist/$(BINARY_NAME)-macos-amd64

macos-arm64: $(SOURCES)
	GOOS=darwin GOARCH=arm64 go build -trimpath -o dist/$(BINARY_NAME)-macos-arm64

clean:
	rm -f $(BINARY_NAME) dist/$(BINARY_NAME)-*