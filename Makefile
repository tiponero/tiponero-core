TAILWIND := tailwindcss
BINARY := bin/tiponero

.PHONY: generate css build dev lint clean

generate:
	go -C tools tool templ generate -path ..

css:
	$(TAILWIND) -i static/css/input.css -o static/css/output.css --minify

build: generate css
	go build -o $(BINARY) ./cmd/tiponero

dev:
	go -C tools tool air

lint:
	go -C tools tool golangci-lint run

clean:
	rm -rf $(BINARY) tmp static/css/output.css
	find . -name "*_templ.go" -delete
