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
	Level            string // log level
	Verbose          bool   // includes log caller information
	Path             string // log file path
	MaxAgeHour       int    // max age for clean up expired log
	RotationTimeHour int    // time interval of rotating log
}

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
