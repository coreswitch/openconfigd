// Copyright 2017 CoreSwitch
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
	"testing"
)

func TestProcessStart(t *testing.T) {
	args := []string{"-d", "-f"}
	proc1 := NewProcess("dhcp", args...)
	ProcessRegister(proc1)
	proc2 := NewProcess("dhcp", args...)
	ProcessRegister(proc2)
	fmt.Println(ProcessCount())

	ProcessUnregister(proc2)
	fmt.Println(ProcessCount())
	proc3 := NewProcess("dhcp", args...)
	ProcessUnregister(proc3)
	fmt.Println(ProcessCount())

	ProcessUnregister(proc1)
	fmt.Println(ProcessCount())
}
