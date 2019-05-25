package expect

import (
	"regexp"
	"time"
)

func createLogManager() *logManager {
	rv := &logManager{
		setLogger: make(chan Logger),
		messages:  make(chan func(Logger)),
		quit:      make(chan struct{}),
	}

	rv.start()

	return rv
}

// logManager is used internally to buffer logs, and execute all Logger messages in a single GoRoutine.
//
// Note: Close() must be called for proper garbage collection
type logManager struct {
	setLogger chan Logger
	messages  chan func(Logger)
	quit      chan struct{}
}

func (manager *logManager) Send(msg []byte) {
	t := time.Now()
	manager.messages <- func(logger Logger) {
		logger.Send(t, msg)
	}
}

func (manager *logManager) SendMasked(msg []byte) {
	t := time.Now()
	manager.messages <- func(logger Logger) {
		logger.SendMasked(t, msg)
	}
}

func (manager *logManager) Recv(msg []byte) {
	t := time.Now()
	manager.messages <- func(logger Logger) {
		logger.Recv(t, msg)
	}
}

func (manager *logManager) RecvNet(msg []byte) {
	t := time.Now()
	manager.messages <- func(logger Logger) {
		logger.RecvNet(t, msg)
	}
}

func (manager *logManager) RecvEOF() {
	t := time.Now()
	manager.messages <- func(logger Logger) {
		logger.RecvEOF(t)
	}
}

func (manager *logManager) ExpectCall(regexp *regexp.Regexp) {
	t := time.Now()
	manager.messages <- func(logger Logger) {
		logger.ExpectCall(t, regexp)
	}
}

func (manager *logManager) ExpectReturn(m Match, err error) {
	t := time.Now()
	manager.messages <- func(logger Logger) {
		logger.ExpectReturn(t, m, err)
	}
}

// Note:
//  Close also ends managerProcess()
func (manager *logManager) Close() {
	t := time.Now()
	manager.messages <- func(logger Logger) {
		logger.Close(t)
	}

	// Tell log buffer to exit (causing the log writer to exit)
	manager.quit <- struct{}{}

	// Wait for log writer to tell us that it exited
	_, ok := <-manager.quit
	if ok {
		panic("Expect internal error: manager.quit should have been closed")
	}
}

func (manager *logManager) SetLogger(logger Logger) {
	manager.setLogger <- logger
}

type logAction struct {
	msg    func(Logger)
	logger Logger
}

func (action *logAction) run() {
	action.msg(action.logger)
}

func (manager *logManager) start() {
	writerChan := make(chan logAction)

	// logWriter. This is the thing that takes a log action (whatever it is) and runs it in a single thread.
	go func() {
		for action := range writerChan {
			action.run()
		}

		// Signal Close() that we're done
		close(manager.quit)
	}()

	// Buffer GoRoutine
	go func() {
		var logger Logger

		var queue []func(Logger)
		done := false
		for !done {

			// Only send action to the writer if we have a message in the queue
			var sendChan chan logAction
			var sendMsg logAction
			if logger != nil && len(queue) > 0 {
				sendChan = writerChan

				// Create action
				sendMsg = logAction{
					msg:    queue[0],
					logger: logger,
				}
			}

			// I/O
			select {
			case <-manager.quit:
				// End the process
				done = true

			case msg := <-manager.messages:
				// Recv a message
				queue = append(queue, msg)

			case logger = <-manager.setLogger:
				// Logger was set/changed

			case sendChan <- sendMsg:
				// Message was successfully sent from queue
				queue = queue[1:]
			}
		}

		// Drain queue if we have a logger, skip if we don't
		if logger != nil {
			for _, msg := range queue {
				writerChan <- logAction{
					msg:    msg,
					logger: logger,
				}
			}
		}

		// Shut down the log writer
		close(writerChan)
	}()
}
