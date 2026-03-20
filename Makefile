CMDS := $(notdir $(wildcard cmd/*))
BINS := $(CMDS:%=bin/%)

.PHONY: all clean

all: $(BINS)

bin/%: cmd/%/main.go
	@mkdir -p bin
	go build -o $@ ./cmd/$*/

clean:
	rm -rf bin
