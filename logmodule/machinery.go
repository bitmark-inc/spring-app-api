package logmodule

import (
	"github.com/sirupsen/logrus"
)

// MachineryLogger custom log module for machinery
type MachineryLogger struct {
	Prefix string
}

func (l *MachineryLogger) Print(args ...interface{}) {
	logrus.WithField("prefix", l.Prefix).Print(args...)
}

func (l *MachineryLogger) Printf(format string, args ...interface{}) {
	logrus.WithField("prefix", l.Prefix).Printf(format, args...)
}

func (l *MachineryLogger) Println(args ...interface{}) {
	logrus.WithField("prefix", l.Prefix).Println(args...)
}

func (l *MachineryLogger) Fatal(args ...interface{}) {
	logrus.WithField("prefix", l.Prefix).Fatal(args...)
}

func (l *MachineryLogger) Fatalf(format string, args ...interface{}) {
	logrus.WithField("prefix", l.Prefix).Fatalf(format, args...)
}

func (l *MachineryLogger) Fatalln(args ...interface{}) {
	logrus.WithField("prefix", l.Prefix).Fatalln(args...)
}

func (l *MachineryLogger) Panic(args ...interface{}) {
	logrus.WithField("prefix", l.Prefix).Panic(args...)
}

func (l *MachineryLogger) Panicf(format string, args ...interface{}) {
	logrus.WithField("prefix", l.Prefix).Panicf(format, args...)
}

func (l *MachineryLogger) Panicln(args ...interface{}) {
	logrus.WithField("prefix", l.Prefix).Panicln(args...)
}
