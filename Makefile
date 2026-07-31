.PHONY: proto proto-protoc tidy run build podman-up podman-down podman-build quadlet-install quadlet-build quadlet-enable
 
# Preferred: generate gRPC/protobuf Go code with buf (https://buf.build).
# Requires network access to buf's remote plugins (or local protoc plugins,
# see proto-protoc below).
proto:
	buf generate
 
# Fallback: generate with local protoc + protoc-gen-go + protoc-gen-go-grpc.
# Install plugins first:
#   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
#   go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
proto-protoc:
	protoc \
		--go_out=gen --go_opt=paths=source_relative \
		--go-grpc_out=gen --go-grpc_opt=paths=source_relative,require_unimplemented_servers=false \
		-I proto proto/tsx/v1/tsx.proto
 
tidy:
	go mod tidy
 
build: proto
	go build -o bin/tsx-tracker ./cmd/server
 
run: proto
	go run ./cmd/server
 
podman-build:
	podman-compose -f podman-compose.yml build
 
podman-up:
	podman-compose -f podman-compose.yml up -d
 
podman-down:
	podman-compose -f podman-compose.yml down
 
quadlet-install:
	mkdir -p ~/.config/containers/systemd
	mkdir -p ~/.config/tsx-tracker
	cp deploy/quadlet/tsx-tracker-rootless.container ~/.config/containers/systemd/tsx-tracker.container
	cp -n .env.podman ~/.config/tsx-tracker/.env.podman 2>/dev/null || true
	chmod 600 ~/.config/tsx-tracker/.env.podman
	systemctl --user daemon-reload
 
quadlet-build:
	podman build --no-cache -t localhost/tsx-tracker:latest .
 
quadlet-enable: quadlet-build
	systemctl --user daemon-reload
	systemctl --user start tsx-tracker.service

