.PHONY: ui build test clean dev-ui all install uninstall

BINARY  = eag
VERSION = 0.1.1
PREFIX  = /opt/vectorcore
BINDIR  = $(PREFIX)/bin
ETCDIR  = $(PREFIX)/etc
LOGDIR  = $(PREFIX)/log
SYSTEMD = /lib/systemd/system/

all: ui build

# Build the React UI (required before `make build`)
ui:
	cd web && ([ -f package-lock.json ] && npm ci || npm install) && npm run build

# Build the Go binary (embeds web/dist)
build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/$(BINARY) ./cmd/eag

# Run tests
test:
	go test ./...

# Start Vite dev server (proxies API to localhost:8080)
dev-ui:
	cd web && npm run dev

clean:
	rm -rf bin/ web/dist/

install: build
	install -d $(BINDIR)
	install -d $(ETCDIR)
	install -d $(LOGDIR)
	install -m755 bin/$(BINARY) $(BINDIR)/$(BINARY)
	@if [ ! -f $(ETCDIR)/eag.yaml ]; then \
		install -m644 config.yaml $(ETCDIR)/eag.yaml; \
	fi
	touch $(LOGDIR)/eag.log
	chmod 644 $(LOGDIR)/eag.log
	install -d /lib/systemd/system
	install -m644 systemd/vectorcore-eag.service $(SYSTEMD)/vectorcore-eag.service
	systemctl daemon-reload
	systemctl enable vectorcore-eag
	systemctl start vectorcore-eag

uninstall:
	systemctl stop vectorcore-eag || true
	systemctl disable vectorcore-eag || true
	rm -f $(BINDIR)/$(BINARY)
	rm -f $(SYSTEMD)/vectorcore-eag.service
	systemctl daemon-reload
