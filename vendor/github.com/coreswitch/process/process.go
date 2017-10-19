// Copyright 2017 CoreSwitch.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package process

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/context"
)

type Process struct {
	Name        string
	Vrf         string
	Args        []string
	File        string
	ErrLookup   string
	ErrStart    string
	ErrWait     string
	ExitFunc    func()
	State       int
	Cmd         *exec.Cmd
	StartTimer  int
	RetryTimer  int
	Index       int
	KillPidFile string
}

type ProcessSlice []*Process

const (
	PROCESS_STARTING = iota
	PROCESS_RUNNING
	PROCESS_RETRY
	PROCESS_EXIT_CALLED
	PROCESS_STOP_WAIT
	PROCESS_STOP
)

var (
	ProcessList  = ProcessSlice{}
	ProcessMutex sync.RWMutex
	ProcessDebug bool = true
)

var ProcessStateStr = map[int]string{
	PROCESS_STARTING:    "Starting",
	PROCESS_RUNNING:     "Running",
	PROCESS_RETRY:       "Retry",
	PROCESS_EXIT_CALLED: "Exit Called",
	PROCESS_STOP_WAIT:   "Stop Wait",
	PROCESS_STOP:        "Stop",
}

func NewProcess(name string, args ...string) *Process {
	proc := &Process{
		Name:       name,
		Args:       []string(args),
		RetryTimer: 1,
	}
	return proc
}

func ProcessRegister(proc *Process) {
	if ProcessDebug {
		fmt.Println("[proc]ProcessRegister:", proc.Name)
	}
	ProcessMutex.Lock()
	defer ProcessMutex.Unlock()

	proc.Index = len(ProcessList)
	ProcessList = append(ProcessList, proc)
	proc.Start()
}

func ProcessUnregister(proc *Process) {
	proc.Debug("Unregister", "function is called")
	ProcessMutex.Lock()
	defer ProcessMutex.Unlock()

	proc.Stop()

	index := 0
	procList := ProcessSlice{}
	for _, p := range ProcessList {
		if p != proc {
			if p.Index != index {
				p.Debug("Unregister", fmt.Sprintf("Re-index %d -> %d", p.Index, index))
			}
			p.Index = index
			index++
			procList = append(procList, p)
		}
	}
	ProcessList = procList
}

func ProcessCount() int {
	ProcessMutex.Lock()
	defer ProcessMutex.Unlock()

	return len(ProcessList)
}

func ProcessLookup(index int) *Process {
	if index < 0 {
		if ProcessDebug {
			fmt.Println("[proc]ProcessStart index is less than 0")
		}
		return nil
	}
	if len(ProcessList) < index+1 {
		if ProcessDebug {
			fmt.Println("[proc]ProcessStart index is out of range")
		}
		return nil
	}
	proc := ProcessList[index]
	if proc == nil {
		if ProcessDebug {
			fmt.Println("[proc]ProcessStart process does not exists")
		}
		return nil
	}
	return proc
}

func ProcessStart(index int) {
	if ProcessDebug {
		fmt.Println("[proc]ProcessStart index", index)
	}
	ProcessMutex.Lock()
	defer ProcessMutex.Unlock()

	proc := ProcessLookup(index)
	if proc == nil {
		return
	}
	proc.Start()
}

func ProcessStop(index int) {
	if ProcessDebug {
		fmt.Println("[proc]ProcessStop index", index)
	}
	ProcessMutex.Lock()
	defer ProcessMutex.Unlock()

	proc := ProcessLookup(index)
	if proc == nil {
		return
	}
	proc.Stop()
}

func (proc *Process) Debug(funcName string, message string) {
	if !ProcessDebug {
		return
	}
	fmt.Printf("[proc]%s(%s:%d): %s\n", funcName, proc.Name, proc.Index, message)
}

func (proc *Process) Start() {
	proc.Debug("Start", "function is called")

	if proc.ExitFunc != nil {
		proc.Debug("Start", "process already running, return at here")
		return
	}

	proc.State = PROCESS_STOP
	binary, err := exec.LookPath(proc.Name)
	if err != nil {
		proc.Debug("Start", "LookPath error, return at here")
		proc.ErrLookup = err.Error()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			proc.State = PROCESS_STARTING
			proc.Debug("Start", "PROCESS_STARTING")

			if proc.File != "" {
				os.OpenFile(proc.File, os.O_RDWR|os.O_CREATE, 0644)
			}

			cmd := exec.CommandContext(ctx, binary, proc.Args...)

			env := os.Environ()
			if proc.Vrf != "" {
				env = append(env, fmt.Sprintf("VRF=%s", proc.Vrf))
				env = append(env, "LD_PRELOAD=/usr/bin/vrf_socket.so")
			}
			cmd.Env = env
			proc.Cmd = cmd

			if proc.StartTimer != 0 {
				proc.Debug("Start", fmt.Sprintf("StartTimer %d", proc.StartTimer))
				startTimer := time.NewTimer(time.Duration(proc.StartTimer) * time.Second)
				select {
				case <-startTimer.C:
					proc.Debug("Start", "StartTimer expired")
				case <-done:
					proc.Debug("Start", "Done during StartTimer")
					startTimer.Stop()
					return
				}
			}

			proc.Debug("Start", fmt.Sprintf("%v %v", cmd.Path, cmd.Args))
			err = cmd.Start()
			if err != nil {
				proc.Debug("Start", fmt.Sprintf("%s %v", "cmd.Start()", err))
				proc.ErrStart = err.Error()
			}

			proc.State = PROCESS_RUNNING
			proc.Debug("Start", "PROCESS_RUNNING")

			err = cmd.Wait()
			if err != nil {
				proc.Debug("Start", fmt.Sprintf("%s %v", "cmd.Wait():", err))
				proc.ErrWait = err.Error()
			}

			proc.State = PROCESS_RETRY
			proc.Debug("Start", "PROCESS_RETRY")

			retryTimer := time.NewTimer(time.Duration(proc.RetryTimer) * time.Second)
			select {
			case <-retryTimer.C:
			case <-done:
				retryTimer.Stop()
				return
			}
		}
	}()

	proc.ExitFunc = func() {
		proc.State = PROCESS_EXIT_CALLED
		proc.Debug("ExitFunc", "PROCESS_EXIT_CALLED")
		close(done)
		cancel()
		proc.State = PROCESS_STOP_WAIT
		wg.Wait()
		proc.State = PROCESS_STOP
		proc.Debug("ExitFunc", "PROCESS_STOP")
		if proc.KillPidFile != "" {
			byte, err := ioutil.ReadFile(proc.KillPidFile)
			if err != nil {
				proc.Debug("ExitFunc", err.Error())
			}
			pid := strings.TrimSpace(string(byte))
			exec.Command("kill", "-s", "TERM", pid).Run()
			proc.Debug("ExitFunc", "KillPidFile:"+pid)
		}
	}
}

func (proc *Process) Stop() {
	proc.Debug("Stop", "function is called")
	if proc.ExitFunc != nil {
		proc.ExitFunc()
		proc.ExitFunc = nil
	}
}

func ProcessListShow() string {
	str := ""
	for pos, proc := range ProcessList {
		str += fmt.Sprintf("%d %s", pos, proc.Name)
		if proc.Vrf != "" {
			str += fmt.Sprintf("@%s", proc.Vrf)
		}
		str += fmt.Sprintf(": %s", ProcessStateStr[proc.State])
		if proc.State == PROCESS_RUNNING && proc.Cmd != nil && proc.Cmd.Process != nil {
			str += fmt.Sprintf(" (pid %d)", proc.Cmd.Process.Pid)
		}
		str += "\n"
		if proc.ErrLookup != "" {
			str += fmt.Sprintf("  Last Lookup Error: %s\n", proc.ErrLookup)
		}
		if proc.ErrStart != "" {
			str += fmt.Sprintf("  Last Start Error: %s\n", proc.ErrStart)
		}
		if proc.ErrWait != "" {
			str += fmt.Sprintf("  Last Wait Error: %s\n", proc.ErrWait)
		}
		str += fmt.Sprintf("  %s\n", proc.Args)
	}
	return str
}
