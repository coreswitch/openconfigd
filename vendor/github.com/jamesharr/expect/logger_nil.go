package expect

import (
	"regexp"
	"time"
)

type NilLogger struct{}

func (*NilLogger) Send(time.Time, []byte)               {}
func (*NilLogger) SendMasked(time.Time, []byte)         {}
func (*NilLogger) Recv(time.Time, []byte)               {}
func (*NilLogger) EOF(time.Time)                        {}
func (*NilLogger) ExpectCall(time.Time, *regexp.Regexp) {}
func (*NilLogger) ExpectReturn(time.Time, Match, error) {}
func (*NilLogger) Close(time.Time)                      {}
