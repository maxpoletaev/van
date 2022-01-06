.DEFAULT_GOAL := help
pkgname = github.com/maxpoletaev/van

.PHONY: help
help:
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
	| sed -n 's/^\(.*\): \(.*\)## \(.*\)/\1;\3/p' \
	| column -t  -s ';'

.PHONY: test
test:  ## run go tests
	go test -v -race -timeout 30s

.PHONY: bench
bench:  ## run benchmarks
	go test -bench=. -run=^$$ -benchmem -cpuprofile=cpu.pprof -memprofile=mem.pprof

.PHONY: benchcmp
benchcmp:  ## run benchmarks and compare with the previous benchcmp run
	[ -f bench.txt ] && mv bench.txt bench.old.txt || true
	go test -bench=. -run=^$$ -benchmem > bench.txt
	@benchstat bench.old.txt bench.txt

.PHONY: godoc
godoc:  ## start godoc server at :8000
	@(sleep 1; open http://localhost:8000/pkg/$(pkgname)) &
	@godoc -http=127.0.0.1:8000
