BIN := huangjiu
CC ?= cc
AR ?= ar
GO ?= go

.PHONY: all build clean run test

all: build

native/libhuangjiu_core.a: native/core.c native/core.h
	$(CC) -O2 -Wall -Wextra -c -o native/core.o native/core.c
	$(AR) rcs $@ native/core.o

build: native/libhuangjiu_core.a
	CGO_ENABLED=1 $(GO) build -trimpath -o $(BIN) .

run: build
	./$(BIN)

test: native/libhuangjiu_core.a
	$(GO) test ./...

clean:
	rm -f $(BIN) native/libhuangjiu_core.a native/core.o
