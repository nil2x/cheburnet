NAME=cheburnet
VERSION=1.1
BIN_DIR=bin
DIST_DIR=dist

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(NAME) .

bin:
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 go build -o $(BIN_DIR)/$(NAME)-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -o $(BIN_DIR)/$(NAME)-linux-arm64 .
	GOOS=darwin GOARCH=amd64 go build -o $(BIN_DIR)/$(NAME)-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build -o $(BIN_DIR)/$(NAME)-darwin-arm64 .
	GOOS=windows GOARCH=amd64 go build -o $(BIN_DIR)/$(NAME)-windows-amd64.exe .
	GOOS=windows GOARCH=arm64 go build -o $(BIN_DIR)/$(NAME)-windows-arm64.exe .
	GOOS=android GOARCH=arm64 go build -o $(BIN_DIR)/$(NAME)-android-arm64 .

dist: bin
	@rm -rf $(DIST_DIR)/tmp
	@mkdir -p $(DIST_DIR)/tmp

	cp $(BIN_DIR)/$(NAME)-linux-amd64 $(DIST_DIR)/tmp/$(NAME)
	cp README.md assets/config.json assets/stub.jpg assets/stub.mp4 assets/systemd.txt assets/logrotate.txt $(DIST_DIR)/tmp
	tar -czf $(DIST_DIR)/$(NAME)-$(VERSION)-linux-amd64.tar.gz -C $(DIST_DIR)/tmp .

	@rm -rf $(DIST_DIR)/tmp
	@mkdir -p $(DIST_DIR)/tmp

	cp $(BIN_DIR)/$(NAME)-linux-arm64 $(DIST_DIR)/tmp/$(NAME)
	cp README.md assets/config.json assets/stub.jpg assets/stub.mp4 assets/systemd.txt assets/logrotate.txt $(DIST_DIR)/tmp
	tar -czf $(DIST_DIR)/$(NAME)-$(VERSION)-linux-arm64.tar.gz -C $(DIST_DIR)/tmp .

	@rm -rf $(DIST_DIR)/tmp
	@mkdir -p $(DIST_DIR)/tmp

	cp $(BIN_DIR)/$(NAME)-darwin-amd64 $(DIST_DIR)/tmp/$(NAME)
	cp README.md assets/config.json assets/stub.jpg assets/stub.mp4 $(DIST_DIR)/tmp
	tar -czf $(DIST_DIR)/$(NAME)-$(VERSION)-macos-amd64.tar.gz -C $(DIST_DIR)/tmp .

	@rm -rf $(DIST_DIR)/tmp
	@mkdir -p $(DIST_DIR)/tmp

	cp $(BIN_DIR)/$(NAME)-darwin-arm64 $(DIST_DIR)/tmp/$(NAME)
	cp README.md assets/config.json assets/stub.jpg assets/stub.mp4 $(DIST_DIR)/tmp
	tar -czf $(DIST_DIR)/$(NAME)-$(VERSION)-macos-arm64.tar.gz -C $(DIST_DIR)/tmp .

	@rm -rf $(DIST_DIR)/tmp
	@mkdir -p $(DIST_DIR)/tmp

	cp $(BIN_DIR)/$(NAME)-windows-amd64.exe $(DIST_DIR)/tmp/$(NAME).exe
	cp README.md assets/config.json assets/stub.jpg assets/stub.mp4 assets/secret.bat assets/version.bat $(DIST_DIR)/tmp
	cd $(DIST_DIR)/tmp && zip -q ../$(NAME)-$(VERSION)-windows-amd64.zip *

	@rm -rf $(DIST_DIR)/tmp
	@mkdir -p $(DIST_DIR)/tmp

	cp $(BIN_DIR)/$(NAME)-windows-arm64.exe $(DIST_DIR)/tmp/$(NAME).exe
	cp README.md assets/config.json assets/stub.jpg assets/stub.mp4 assets/secret.bat assets/version.bat $(DIST_DIR)/tmp
	cd $(DIST_DIR)/tmp && zip -q ../$(NAME)-$(VERSION)-windows-arm64.zip *

	@rm -rf $(DIST_DIR)/tmp
	@mkdir -p $(DIST_DIR)/tmp

	cp $(BIN_DIR)/$(NAME)-android-arm64 $(DIST_DIR)/tmp/$(NAME)
	cp README.md assets/config.json assets/stub.jpg assets/stub.mp4 $(DIST_DIR)/tmp
	tar -czf $(DIST_DIR)/$(NAME)-$(VERSION)-android-arm64.tar.gz -C $(DIST_DIR)/tmp .

	@rm -rf $(DIST_DIR)/tmp

	cd $(DIST_DIR) && shasum -a 256 *.tar.gz *.zip > $(NAME)-$(VERSION)-checksums.txt

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)

run:
	go run . -config .vscode/config.json

run-a:
	go run . -config .vscode/config.a.json

run-b:
	go run . -config .vscode/config.b.json
