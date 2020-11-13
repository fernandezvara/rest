package rest

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logging abstracts all log facilities simplifying its usage
// other libraries that need to log something will use this
// to have just one way to do the things
type Logging struct {
	logger *zap.Logger
}

// NewLogging returns a loggin instance, if no output paths passed it will default to stderr
func NewLogging(outputPaths, errorOutputPaths []string, debug bool) (*Logging, error) {

	var (
		l           Logging
		loggerLevel zap.AtomicLevel
		loggerConf  zap.Config
		err         error
	)

	if len(outputPaths) == 0 {
		outputPaths = []string{"stderr"}
	}

	if len(errorOutputPaths) == 0 {
		errorOutputPaths = []string{"stderr"}
	}

	if debug {
		loggerLevel = zap.NewAtomicLevelAt(zap.DebugLevel)
	} else {
		loggerLevel = zap.NewAtomicLevelAt(zap.InfoLevel) // Default logger level (as production)
	}

	loggerConf = zap.Config{
		Level:             loggerLevel,
		Development:       debug,
		DisableCaller:     true,
		DisableStacktrace: debug,
		Encoding:          "json",
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:        "ts",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      "caller",
			MessageKey:     "msg",
			StacktraceKey:  "stacktrace",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeTime:     zapcore.ISO8601TimeEncoder,
			EncodeDuration: zapcore.SecondsDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		},
		OutputPaths:      outputPaths,
		ErrorOutputPaths: errorOutputPaths,
		Sampling: &zap.SamplingConfig{
			Initial:    100,
			Thereafter: 100,
		},
	}

	l.logger, err = loggerConf.Build()

	return &l, err

}

// Logger returns the raw logger
func (l *Logging) Logger() *zap.Logger {
	return l.logger
}

// Error writes an error trace
func (l *Logging) Error(source, msg string, fields ...zap.Field) {
	fields = append(fields, zap.String("source", source))
	l.logger.Error(msg,
		fields...,
	)
}

// Info writes an info trace
func (l *Logging) Info(source, msg string, fields ...zap.Field) {
	fields = append(fields, zap.String("source", source))
	l.logger.Info(msg,
		fields...,
	)
}

// Debug writes a debug trace
func (l *Logging) Debug(source, msg string, fields ...zap.Field) {
	fields = append(fields, zap.String("source", source))
	l.logger.Debug(msg,
		fields...,
	)
}
