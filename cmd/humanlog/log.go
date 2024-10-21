package main

import (
	"log"
	"log/slog"
	"os"
)

type loglevel int

const (
	debuglvl loglevel = iota
	infolvl
	warnlvl
	errorlvl
)

var logLevel = func() loglevel {
	switch os.Getenv("HUMANLOG_LOG_LEVEL") {
	case "debug":
		return debuglvl
	case "info":
		return infolvl
	case "warn":
		return warnlvl
	case "error":
		return errorlvl
	default:
		return infolvl
	}
}()

func slogLevel() slog.Level {
	switch logLevel {
	case debuglvl:
		return slog.LevelDebug
	case infolvl:
		return slog.LevelInfo
	case warnlvl:
		return slog.LevelWarn
	case errorlvl:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func logdebug(format string, args ...interface{}) {
	if logLevel <= debuglvl {
		log.Printf("[debug] "+format, args...)
	}
}

func loginfo(format string, args ...interface{}) {
	if logLevel <= infolvl {
		log.Printf("[info] "+format, args...)
	}
}

func logwarn(format string, args ...interface{}) {
	if logLevel <= warnlvl {
		log.Printf("[warn] "+format, args...)
	}
}

func logerror(format string, args ...interface{}) {
	if logLevel <= errorlvl {
		log.Printf("[error] "+format, args...)
	}
}
