package main

import (
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// snip from kilonova

func initLogger(debug bool) error {
	var encConf zapcore.EncoderConfig
	if debug {
		encConf = zap.NewDevelopmentEncoderConfig()
	} else {
		encConf = zap.NewDevelopmentEncoderConfig()
		// encConf = zap.NewProductionEncoderConfig()
	}
	encConf.EncodeTime = zapcore.TimeEncoder(func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(t.UTC().Format(time.RFC3339))
	})
	encConf.EncodeLevel = zapcore.CapitalColorLevelEncoder

	level := zapcore.InfoLevel
	if debug {
		level = zapcore.DebugLevel
	}

	core := zapcore.NewCore(zapcore.NewConsoleEncoder(encConf), zapcore.AddSync(os.Stdout), level)
	logg := zap.New(core, zap.AddCaller())

	zap.ReplaceGlobals(logg)

	return nil
}

func init() {
	initLogger(true)
}
