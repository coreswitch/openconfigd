package expect

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"syscall"
	"time"

	"github.com/kr/pty"
)

// Expect is a program interaction session.
type Expect struct {
	timeout time.Duration
	pty     io.ReadWriteCloser
	killer  func()
	buffer  []byte

	// channel for receiving read events
	readChan   chan readEvent
	readStatus error

	// Logging helper
	log *logManager
}

// Match is returned from exp.Expect*() when a match is found.
type Match struct {
	Before string
	Groups []string
}

type readEvent struct {
	buf    []byte
	status error
}

// ErrTimeout is returned from exp.Expect*() when a timeout is reached.
var ErrTimeout = errors.New("Expect Timeout")

// READ_SIZE is the largest amount of data expect attempts to read in a single I/O operation.
// This likely needs some research and tuning.
const READ_SIZE = 4094

// Create an Expect instance from a command.
// Effectively the same as Create(pty.Start(exec.Command(name, args...)))
func Spawn(name string, args ...string) (*Expect, error) {
	cmd := exec.Command(name, args...)
	pty, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	killer := func() {
		cmd.Process.Kill()
		// the process is killed, however keeps a defunct process in memory
		go cmd.Process.Wait()
	}
	return Create(pty, killer), nil
}

// Create an Expect instance from something that we can do read/writes off of.
//
// Note: Close() must be called to cleanup this process.
func Create(pty io.ReadWriteCloser, killer func()) (exp *Expect) {
	rv := Expect{
		timeout:  time.Hour * 24 * 365,
		pty:      pty,
		readChan: make(chan readEvent),
		log:      createLogManager(),
		killer:   killer,
	}

	// Start up processes
	rv.startReader()

	// Done
	return &rv
}

// Timeout() returns amount of time an Expect() call will wait for the output to appear.
func (exp *Expect) Timeout() time.Duration {
	return exp.timeout
}

// SetTimeout(Duration) sets the amount of time an Expect() call will wait for the output to appear.
func (exp *Expect) SetTimeout(d time.Duration) {
	exp.timeout = d
}

// Return the current buffer.
//
// Note: This is not all data received off the network, but data that has been received for processing.
func (exp *Expect) Buffer() []byte {
	return exp.buffer
}

// Kill & close off process.
//
// Note: This *must* be run to cleanup the process
func (exp *Expect) Close() error {
	exp.killer()
	err := exp.pty.Close()
	for readEvent := range exp.readChan {
		exp.mergeRead(readEvent)
	}
	exp.log.Close()
	return err
}

// Send data to program
func (exp *Expect) Send(s string) error {
	return exp.send([]byte(s), false)
}

// Send data, but mark it as masked to observers. Use this for passwords
func (exp *Expect) SendMasked(s string) error {
	return exp.send([]byte(s), true)
}

// Send several lines data (separated by \n) to the process
func (exp *Expect) SendLn(lines ...string) error {
	for _, l := range lines {
		if err := exp.Send(l + "\n"); err != nil {
			return err
		}
	}
	return nil
}

func (exp *Expect) send(arr []byte, masked bool) error {
	for len(arr) > 0 {
		if n, err := exp.pty.Write(arr); err == nil {
			if masked {
				exp.log.SendMasked(arr[0:n])
			} else {
				exp.log.Send(arr[0:n])
			}
			arr = arr[n:]
		} else {
			return err
		}
	}
	return nil
}

// ExpectRegexp searches the I/O read stream for a pattern within .Timeout()
func (exp *Expect) ExpectRegexp(pat *regexp.Regexp) (Match, error) {
	exp.log.ExpectCall(pat)

	// Read error happened.
	if exp.readStatus != nil {
		exp.log.ExpectReturn(Match{}, exp.readStatus)
		return Match{}, exp.readStatus
	}

	// Calculate absolute timeout
	giveUpTime := time.Now().Add(exp.timeout)

	// Loop until we match or read some data
	for first := true; first || time.Now().Before(giveUpTime); first = false {
		// Read some data
		if !first {
			exp.readData(giveUpTime)
		}

		// Check for match
		if m, found := exp.checkForMatch(pat); found {
			exp.log.ExpectReturn(m, nil)
			return m, nil
		}

		// If no match, check for read error (likely io.EOF)
		if exp.readStatus != nil {
			exp.log.ExpectReturn(Match{}, exp.readStatus)
			return Match{}, exp.readStatus
		}
	}

	// Time is up
	exp.log.ExpectReturn(Match{}, ErrTimeout)
	return Match{}, ErrTimeout
}

func (exp *Expect) checkForMatch(pat *regexp.Regexp) (m Match, found bool) {

	matches := pat.FindSubmatchIndex(exp.buffer)
	if matches != nil {
		found = true
		groupCount := len(matches) / 2
		m.Groups = make([]string, groupCount)

		for i := 0; i < groupCount; i++ {
			start := matches[2*i]
			end := matches[2*i+1]
			if start >= 0 && end >= 0 {
				m.Groups[i] = string(exp.buffer[start:end])
			}
		}
		m.Before = string(exp.buffer[0:matches[0]])
		exp.buffer = exp.buffer[matches[1]:]
	}
	return
}

func (exp *Expect) readData(giveUpTime time.Time) {
	wait := giveUpTime.Sub(time.Now())
	select {
	case read, ok := <-exp.readChan:
		if ok {
			exp.mergeRead(read)
		}

	case <-time.After(wait):
		// Timeout & return
	}
}

func (exp *Expect) mergeRead(read readEvent) {
	exp.buffer = append(exp.buffer, read.buf...)
	exp.readStatus = read.status
	exp.fixNewLines()

	if len(read.buf) > 0 {
		exp.log.Recv(read.buf)
	}

	if read.status == io.EOF {
		exp.log.RecvEOF()
	}
}

var newLineRegexp *regexp.Regexp
var newLineOnce sync.Once

// fixNewLines will change various newlines combinations to \n
func (exp *Expect) fixNewLines() {
	newLineOnce.Do(func() { newLineRegexp = regexp.MustCompile("\r\n") })

	// This code could probably be optimized
	exp.buffer = newLineRegexp.ReplaceAllLiteral(exp.buffer, []byte("\n"))
}

// Expect(s string) is equivalent to exp.ExpectRegexp(regexp.MustCompile(s))
func (exp *Expect) Expect(expr string) (m Match, err error) {
	return exp.ExpectRegexp(regexp.MustCompile(expr))
}

// Wait for EOF
func (exp *Expect) ExpectEOF() error {
	_, err := exp.Expect("$EOF")
	return err
}

// Set up an I/O logger
func (exp *Expect) SetLogger(logger Logger) {
	if exp.log == nil {
		panic("Expect object is uninitialized")
	}
	exp.log.SetLogger(logger)
}

func (exp *Expect) startReader() {
	bufferInput := make(chan readEvent)

	// Buffer shim
	go func() {
		queue := make([]readEvent, 0)
		done := false

		// Normal I/O loop
		for !done {
			var sendItem readEvent
			var sendChan chan readEvent = nil

			// Set up send operation if we have data to send
			if len(queue) > 0 {
				sendItem = queue[0]
				sendChan = exp.readChan
			}

			// I/O
			select {
			case sendChan <- sendItem:
				queue = queue[1:]
			case read, ok := <-bufferInput:
				if ok {
					queue = append(queue, read)
				} else {
					done = true
				}
			}
		}

		// Drain buffer
		for _, read := range queue {
			exp.readChan <- read
		}

		// Close output
		close(exp.readChan)
	}()

	// Reader process
	go func() {
		done := false
		for !done {
			buf := make([]byte, READ_SIZE)
			n, err := exp.pty.Read(buf)
			buf = buf[0:n]

			// OSX: Closed FD returns io.EOF
			// Linux: Closed FD returns syscall.EIO, translate to io.EOF
			pathErr, ok := err.(*os.PathError)
			if ok && pathErr.Err == syscall.EIO {
				err = io.EOF
			}

			exp.log.RecvNet(buf)
			bufferInput <- readEvent{buf, err}

			if err != nil {
				done = true
			}
		}
		close(bufferInput)
	}()

}
