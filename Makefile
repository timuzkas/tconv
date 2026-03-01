all: build

build:
	go build -o tconv .

run: build
	./tconv

clean:
	rm -f tconv

.PHONY: all build run clean
