package diagnosis

import (
	"fmt"
	rotatelogs "github.com/lestrrat/go-file-rotatelogs"
	"github.com/sirupsen/logrus"
	"io"
	"os"
	"time"
)

const StandOutPutPath = "stdout"

type LogConfig struct {
	Level            string // 日志级别
	Verbose          bool   // 是否记录调用位置
	Path             string // 日志输出路径
	MaxAgeHour       int    // 日志文件最大保留时长
	RotationTimeHour int    // 日志文件轮转周期
}

// InitLogger 根据配置初始化日志级别、输出位置、轮转策略和格式。
func InitLogger(conf *LogConfig) error {
	// parse conf
	var writer io.Writer
	if conf.Path == StandOutPutPath {
		writer = os.Stdout
	} else {
		fileWriter, err := rotatelogs.New(
			conf.Path+".%Y%m%d",
			rotatelogs.WithLinkName(conf.Path),
			rotatelogs.WithMaxAge(time.Hour*time.Duration(conf.MaxAgeHour)),
			rotatelogs.WithRotationTime(time.Hour*time.Duration(conf.RotationTimeHour)))
		if err != nil {
			return fmt.Errorf("log rotation build failed: %w", err)
		}
		writer = fileWriter
	}

	level, err := logrus.ParseLevel(conf.Level)
	if err != nil {
		return fmt.Errorf("log level parse failed: %w", err)
	}

	// set logger
	logrus.SetOutput(writer)
	logrus.SetLevel(level)
	logrus.SetReportCaller(conf.Verbose)
	logrus.SetFormatter(&logrus.TextFormatter{
		ForceQuote: true,
	})

	return nil
}
