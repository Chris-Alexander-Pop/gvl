.PHONY: build install test tidy clean

build:
	go build -o bin/gvl ./cmd/gvl
	go build -o bin/gvld ./cmd/gvld

install: build
	install -m 755 bin/gvl $(HOME)/.local/bin/gvl
	install -m 755 bin/gvld $(HOME)/.local/bin/gvld

test:
	go test ./...

tidy:
	go mod tidy

clean:
	rm -rf bin data
