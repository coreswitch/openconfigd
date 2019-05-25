package expect

import (
	"testing"

	"regexp"
	"time"
)

// Logging adapter for `testing.T` test cases
func TestLogger(t *testing.T) Logger {
	return &testLogger{
		t: t,
	}
}

type testLogger struct {
	t *testing.T
}

func (logger *testLogger) fmtTime(t time.Time) string {
	return t.Format("[15:04:05.999]")
}

func (logger *testLogger) Send(t time.Time, data []byte) {
	logger.t.Logf("%s Send %q", logger.fmtTime(t), string(data))
}

func (logger *testLogger) SendMasked(t time.Time, _ []byte) {
	logger.t.Logf("%s Send %q", logger.fmtTime(t), "*** Masked ***")
}

func (logger *testLogger) Recv(t time.Time, data []byte) {
	logger.t.Logf("%s Recv %q", logger.fmtTime(t), string(data))
}

func (logger *testLogger) RecvNet(t time.Time, data []byte) {
	// Do nothing. This can be added if its needed, but this is likely too verbose.
}

func (logger *testLogger) RecvEOF(t time.Time) {
	logger.t.Logf("%s RecvEOF", logger.fmtTime(t))
}

func (logger *testLogger) ExpectCall(t time.Time, r *regexp.Regexp) {
	logger.t.Logf("%s Expect %v", logger.fmtTime(t), r)
}

func (logger *testLogger) ExpectReturn(t time.Time, m Match, e error) {
	logger.t.Logf("%s ExpectReturn %q %q", logger.fmtTime(t), m, e)
}

func (logger *testLogger) Close(t time.Time) {
	logger.t.Logf("%s Close", logger.fmtTime(t))
}
