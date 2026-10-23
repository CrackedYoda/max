package shared

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const loggerKey = "logger"

func NewLogger(env, level string) (*zap.Logger, error) {
	var cfg zap.Config
	if env == "production" {
		cfg = zap.NewProductionConfig()
	} else {
		cfg = zap.NewDevelopmentConfig()
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	lvl, err := zapcore.ParseLevel(level)
	if err != nil {
		return nil, err
	}
	cfg.Level = zap.NewAtomicLevelAt(lvl)
	// 4xx rejections log at warn; stack traces are only useful for real failures.
	return cfg.Build(zap.AddStacktrace(zapcore.ErrorLevel))
}

func SetLogger(c *gin.Context, l *zap.Logger) {
	c.Set(loggerKey, l)
}

func GetLogger(c *gin.Context) *zap.Logger {
	if v, ok := c.Get(loggerKey); ok {
		if l, ok := v.(*zap.Logger); ok {
			return l
		}
	}
	return zap.L()
}
