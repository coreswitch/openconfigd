package expect

import (
	"regexp"
	"time"
)

type Logger interface {

	// API user sent an item
	Send(time.Time, []byte)

	// API user sent a masked item. The masked data is included, but the API user is advised to
	// not log this data in production.
	SendMasked(time.Time, []byte)

	// Data is received by the same goroutine as the API user.
	Recv(time.Time, []byte)

	// Data is received off the network
	RecvNet(time.Time, []byte)

	// EOF has been reached. Time is when the EOF was received off the network
	RecvEOF(time.Time)

	// API user ran some form of Expect* call
	ExpectCall(time.Time, *regexp.Regexp)

	// API user got a return back from an Expect* call
	ExpectReturn(time.Time, Match, error)

	// Close the log file / this is the last item
	Close(time.Time)
}
