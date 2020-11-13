package debug

import (
	"io/ioutil"
	"log"
	"os"
)

var loggerInfo *log.Logger
var loggerError *log.Logger
var LoggerDebug *log.Logger

var Infof func(format string, v ...interface{})
var Debugf func(format string, v ...interface{})
var Errorf func(format string, v ...interface{})
var Fatalf func(format string, v ...interface{})

func init() {
	loggerInfo = log.New(os.Stdout, "", log.Lshortfile)
	Infof = loggerInfo.Printf
	loggerError = log.New(os.Stderr, "ERROR: ", log.Lshortfile)
	Errorf = loggerError.Printf
	if os.Getenv("DEBUG") == "true" {
		LoggerDebug = log.New(os.Stdout, "DEBUG: ", log.Lshortfile)
	} else {
		LoggerDebug = log.New(ioutil.Discard, "", log.Lshortfile)
	}
	Debugf = LoggerDebug.Printf
	Fatalf = loggerInfo.Fatalf
}
