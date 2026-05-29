set dotenv-load

default:
    @just --list

test:
    @go test -race .

bench:
	@go test -bench=.
