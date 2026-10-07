# SPDX-License-Identifier: GPL-3.0-or-later
# User-local install by default; no root needed:
#     make && make install
# System-wide:
#     make && sudo make install PREFIX=/usr/local

PREFIX  ?= $(HOME)/.local
BINDIR  ?= $(PREFIX)/bin
MANDIR  ?= $(PREFIX)/share/man/man1
DATADIR ?= $(PREFIX)/share/cdir
BASHCOMPDIR ?= $(PREFIX)/share/bash-completion/completions
ZSHCOMPDIR  ?= $(PREFIX)/share/zsh/site-functions

VERSION ?= 1.0.0
GO      ?= go
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build test install uninstall clean

all: build

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o cdir ./cmd/cdir

test:
	$(GO) vet ./...
	$(GO) test ./...

install: build
	install -Dm755 cdir              $(DESTDIR)$(BINDIR)/cdir
	install -Dm644 man/cdir.1        $(DESTDIR)$(MANDIR)/cdir.1
	install -Dm644 shell/bash.sh     $(DESTDIR)$(DATADIR)/bash.sh
	install -Dm644 shell/zsh.sh      $(DESTDIR)$(DATADIR)/zsh.sh
	install -Dm644 completions/bash/cdir $(DESTDIR)$(BASHCOMPDIR)/cdir
	install -Dm644 completions/zsh/_cdir $(DESTDIR)$(ZSHCOMPDIR)/_cdir
	@echo
	@echo "Installed. Now add this line to your shell startup file:"
	@echo "  bash (~/.bashrc): source $(DATADIR)/bash.sh"
	@echo "  zsh  (~/.zshrc):  source $(DATADIR)/zsh.sh"

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/cdir $(DESTDIR)$(MANDIR)/cdir.1 \
	      $(DESTDIR)$(DATADIR)/bash.sh $(DESTDIR)$(DATADIR)/zsh.sh \
	      $(DESTDIR)$(BASHCOMPDIR)/cdir $(DESTDIR)$(ZSHCOMPDIR)/_cdir
	-rmdir $(DESTDIR)$(DATADIR)

clean:
	rm -f cdir
