# SPDX-FileCopyrightText: 2020 Jecoz
#
# SPDX-License-Identifier: MIT

diic: cmd/main.go
	go build -v -o bin/$@ $^
test:
	go test ./...
format:
	go fmt ./...
