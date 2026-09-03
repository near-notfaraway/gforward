package diagnosis

import (
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	. "github.com/smartystreets/goconvey/convey"
)

func TestInitLogger(t *testing.T) {
	Convey("InitLogger should configure the global logger from config", t, func() {
		// 保存并在用例结束后恢复全局 logrus 状态，避免污染其他测试。
		originalLevel := logrus.GetLevel()
		Reset(func() {
			logrus.SetOutput(logrus.StandardLogger().Out)
			logrus.SetLevel(originalLevel)
			logrus.SetReportCaller(false)
		})

		Convey("Stdout path with a valid level should succeed", func() {
			err := InitLogger(&LogConfig{Level: "debug", Path: StandOutPutPath})

			So(err, ShouldBeNil)
			So(logrus.GetLevel(), ShouldEqual, logrus.DebugLevel)
		})

		Convey("An invalid level should return an error", func() {
			err := InitLogger(&LogConfig{Level: "not-a-level", Path: StandOutPutPath})

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "log level parse failed")
		})

		Convey("A file path should build a rotating writer", func() {
			logPath := filepath.Join(t.TempDir(), "gforward.log")

			err := InitLogger(&LogConfig{
				Level:            "warn",
				Path:             logPath,
				MaxAgeHour:       24,
				RotationTimeHour: 1,
			})

			So(err, ShouldBeNil)
			So(logrus.GetLevel(), ShouldEqual, logrus.WarnLevel)
		})
	})
}
