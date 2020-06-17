all: cryptodog-server

cryptodog-server: server.go proto/proto.go
	gofmt -l -s -w .
	go vet
	go build
