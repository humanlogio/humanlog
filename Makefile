all:
	@godep go build -o humanlog cmd/humanlog/*.go

install:
	@godep go install cmd/...
