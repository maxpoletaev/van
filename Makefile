.DEFAULT_GOAL := help

.PHONY: help
help:
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
	| sed -n 's/^\(.*\): \(.*\)## \(.*\)/\1;\3/p' \
	| column -t  -s ';'

.PHONY: test
test:  ## Run go tests
	go test -v -race

.PHONY: bench
bench:  ## Run benchmark
	go test -bench=. -run=^$$ -benchmem -cpuprofile=cpu.pprof -memprofile=mem.pprof

.PHONY: benchcmp
benchcmp:  ## Run benchmark and compare with the previous benchcmp run
	[ -f bench.txt ] && mv bench.txt bench.old.txt || true
	go test -bench=. -run=^$$ -benchmem -cpuprofile=cpu.pprof -memprofile=mem.pprof > bench.txt
	@benchcmp bench.old.txt bench.txt

