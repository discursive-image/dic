export BINDIR ?= $(abspath bin)

PREFIX :=
SRC := google/*.go cmd/*/*.go
TARGETS := dic

BINNAMES := $(addprefix $(PREFIX), $(TARGETS))
BINS := $(addprefix $(BINDIR)/, $(BINNAMES))

all: $(BINS)
test: $(SRC); go test ./...
clean:; rm -rf $(BINDIR)/$(PREFIX)*

$(BINDIR)/$(PREFIX)%: $(SRC); go build -o $@ cmd/$*/main.go
$(BINS): | $(BINDIR)
$(BINDIR):; mkdir -p $(BINDIR)
