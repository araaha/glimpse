# Build and install glimpse with its man page and shell completions.
# Override PREFIX for the install location and DESTDIR for staged installs.
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
DATADIR ?= $(PREFIX)/share
MANDIR ?= $(DATADIR)/man
GO ?= go
BIN ?= glimpse
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null)
LDFLAGS := $(if $(VERSION),-X glimpse/cmd/glimpse/command.Version=$(VERSION))

.PHONY: build test install uninstall

build:
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/glimpse

test:
	$(GO) test -race ./...

install: build
	install -Dm755 $(BIN) $(DESTDIR)$(BINDIR)/glimpse
	install -Dm644 docs/glimpse.1 $(DESTDIR)$(MANDIR)/man1/glimpse.1
	install -d $(DESTDIR)$(DATADIR)/bash-completion/completions \
		$(DESTDIR)$(DATADIR)/zsh/site-functions \
		$(DESTDIR)$(DATADIR)/fish/vendor_completions.d
	$(abspath $(BIN)) --completion bash > $(DESTDIR)$(DATADIR)/bash-completion/completions/glimpse
	$(abspath $(BIN)) --completion zsh > $(DESTDIR)$(DATADIR)/zsh/site-functions/_glimpse
	$(abspath $(BIN)) --completion fish > $(DESTDIR)$(DATADIR)/fish/vendor_completions.d/glimpse.fish

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/glimpse $(DESTDIR)$(MANDIR)/man1/glimpse.1 \
		$(DESTDIR)$(DATADIR)/bash-completion/completions/glimpse \
		$(DESTDIR)$(DATADIR)/zsh/site-functions/_glimpse \
		$(DESTDIR)$(DATADIR)/fish/vendor_completions.d/glimpse.fish
