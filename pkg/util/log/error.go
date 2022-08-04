package log

import (
	"flag"
	"fmt"
	"os"
)

// HandleError prints the error to the standard error, prints the usage if the `printUsage` flag is true,
// exports the error to the error file and exits the process with the exit code.
func HandleError(log *Logger, printUsage bool, format string, a ...interface{}) {
	s := fmt.Sprintf(format, a...)
	fmt.Fprintln(os.Stderr, s)
	if printUsage {
		flag.Usage()
	}
	log.ExportError(s)
	os.Exit(1)
}
