package main

import (
	"log/slog"
	"os"

	"github.com/charmbracelet/log"
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
		log.Debugf(format, args...)
	}
}

func loginfo(format string, args ...interface{}) {
	if logLevel <= infolvl {
		log.Infof(format, args...)
	}
}

func logwarn(format string, args ...interface{}) {
	if logLevel <= warnlvl {
		log.Warnf(format, args...)
	}
}

func logerror(format string, args ...interface{}) {
	if logLevel <= errorlvl {
		log.Errorf(format, args...)
	}
}
