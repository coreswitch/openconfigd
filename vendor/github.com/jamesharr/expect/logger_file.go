package expect

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"time"
)

func StderrLogger() Logger {
	return &fileLogger{
		w:            os.Stderr,
		closeOnClose: false,
	}
}

// Create an appending file logger.
func FileLogger(filename string) Logger {
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0640)
	if err != nil {
		panic(err)
	}
	return &fileLogger{
		w:            file,
		closeOnClose: true,
	}
}

type fileLogger struct {
	w            io.WriteCloser
	closeOnClose bool
}

func fmtTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05.999 -0700 MST")
}

func (logger *fileLogger) Send(t time.Time, data []byte) {
	fmt.Fprintf(logger.w, "%s Send %q\n", fmtTime(t), string(data))
}

func (logger *fileLogger) SendMasked(t time.Time, _ []byte) {
	fmt.Fprintf(logger.w, "%s Send %v\n", fmtTime(t), "*** MASKED ***")
}

func (logger *fileLogger) Recv(t time.Time, data []byte) {
	fmt.Fprintf(logger.w, "%s Recv %q\n", fmtTime(t), string(data))
}

func (logger *fileLogger) RecvNet(t time.Time, data []byte) {
	// This is likely too verbose.
}

func (logger *fileLogger) RecvEOF(t time.Time) {
	fmt.Fprintf(logger.w, "%s EOF\n", fmtTime(t))
}

func (logger *fileLogger) ExpectCall(t time.Time, r *regexp.Regexp) {
	fmt.Fprintf(logger.w, "%s Expect %v\n", fmtTime(t), r)
}

func (logger *fileLogger) ExpectReturn(t time.Time, m Match, e error) {
	fmt.Fprintf(logger.w, "%s ExpectReturn %q %v\n", fmtTime(t), m, e)
}

func (logger *fileLogger) Close(t time.Time) {
	fmt.Fprintf(logger.w, "%s Close\n", fmtTime(t))

	if logger.closeOnClose {
		logger.w.Close()
	}
}
