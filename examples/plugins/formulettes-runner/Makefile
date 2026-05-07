# formulettes is vendored under cpp/formulettes/ so the plugin builds
# from a fresh clone with no upstream Sibelius checkout. Only the
# formulettes subdir was copied — keep additions to that dir to a
# minimum; this is a vendor mirror, not a maintenance fork.
FORM_DIR := cpp/formulettes
CXX      ?= c++
CXXFLAGS ?= -std=c++17 -O2 -Wall

# Most formulettes .cpp files are 1-2 lines wrapping inline-header
# definitions, but Normal.cpp carries the cdf implementation, so
# compile them all and link alongside bs.cpp.
FORM_SRC := $(wildcard $(FORM_DIR)/*.cpp)

bs-cli: cpp/bs.cpp $(FORM_SRC)
	$(CXX) $(CXXFLAGS) -Icpp -o $@ cpp/bs.cpp $(FORM_SRC)

formulettes-runner: main.go
	go build -o $@ .

.PHONY: all clean test
all: bs-cli formulettes-runner

test:
	go test -race ./...

clean:
	rm -f bs-cli formulettes-runner
