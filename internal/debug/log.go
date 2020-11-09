package debug

import (
	"io/ioutil"
	"log"
	"os"
)

var loggerInfo *log.Logger
var loggerError *log.Logger
var loggerDebug *log.Logger

func Infof(format string, v ...interface{}) {
	loggerInfo.Printf(format, v...)
}

func Debugf(format string, v ...interface{}) {
	loggerDebug.Printf(format, v...)
}

func Errorf(format string, v ...interface{}) {
	loggerError.Printf(format, v...)
}

func Fatalf(format string, v ...interface{}) {
	loggerInfo.Fatalf(format, v...)
}

func init() {
	loggerInfo = log.New(os.Stdout, "", log.Lshortfile)
	loggerError = log.New(os.Stderr, "ERROR: ", log.Lshortfile)
	if os.Getenv("DEBUG") == "true" {
		loggerDebug = log.New(os.Stdout, "DEBUG: ", log.Lshortfile)
	} else {
		loggerDebug = log.New(ioutil.Discard, "", log.Lshortfile)
	}
	loggerInfo = log.New(os.Stdout, "", log.Lshortfile)
}
