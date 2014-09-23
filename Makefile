release:
	@godep go build -o humanlog cmd/humanlog/*.go
	@tar czf humanlog_$(GOOS)_$(GOARCH).tar.gz humanlog
	@rm humanlog

all:
	@godep go build -o humanlog cmd/humanlog/*.go

install:
	@godep go install cmd/...
