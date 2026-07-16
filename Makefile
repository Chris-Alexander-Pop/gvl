.PHONY: build install test tidy clean

COMPDIR ?= $(HOME)/.oh-my-zsh/custom/completions

build:
	go build -o bin/gvl ./cmd/gvl
	go build -o bin/gvld ./cmd/gvld

install: build
	install -m 755 bin/gvl $(HOME)/.local/bin/gvl
	install -m 755 bin/gvld $(HOME)/.local/bin/gvld
	@mkdir -p "$(COMPDIR)"
	./bin/gvl completion zsh > "$(COMPDIR)/_gvl"
	@echo "zsh completion → $(COMPDIR)/_gvl  (run: exec zsh)"

test:
	go test ./...

tidy:
	go mod tidy

clean:
	rm -rf bin data
