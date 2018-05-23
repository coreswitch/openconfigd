package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jamesharr/expect"
)

func main() {

	// Start up ssh process
	exp, err := expect.Spawn(
		"ssh",
		"-F", "/dev/null",
		"-o", "UserKnownHostsFile /dev/null",
		"-o", "StricthostKeyChecking false",
		"localhost",
	)
	checkErr(err)

	// Add logger
	exp.SetLogger(expect.FileLogger("ssh.log"))
	//	exp.SetLogger(expect.StderrLogger())

	// Set a timeout
	exp.SetTimeout(5 * time.Second)

	// Loop with until user gets password right
	for loggedIn := false; !loggedIn; {
		m, err := exp.Expect(`[Pp]assword:|\$`)
		checkErr(err)

		if m.Groups[0] == "$" {
			loggedIn = true
		} else {
			password := readPassword()
			exp.SendMasked(password)
			exp.Send("\n")
		}
	}

	// Run a command, chew up echo.
	const CMD = "ls -lh"
	checkErr(exp.SendLn(CMD))
	_, err = exp.Expect(CMD)
	checkErr(err)

	// Expect new prompt, get results from m.Before
	m, err := exp.Expect(`(?m)^.*\$`)
	checkErr(err)
	fmt.Println("Directory Listing:", m.Before)

	// Exit
	checkErr(exp.SendLn("exit"))

	// Remote should close the connection
	err = exp.ExpectEOF()
	if err != io.EOF {
		panic(fmt.Sprintf("Expected EOF, got %v", err))
	}

	// In most cases you'd do this in an 'defer' clause right after it was
	// opened.
	exp.Close()

	// You can use this to see that there's no extra expect processes running
	// time.Sleep(100 * time.Millisecond)
	// panic("DEBUG: Who's running")
}

func readPassword() string {
	fmt.Print("Enter Password: ")

	stdin := bufio.NewReader(os.Stdin)
	password, err := stdin.ReadString('\n')
	fmt.Println()
	if err != nil {
		fmt.Println("ERROR")
		panic(err)
	}
	password = password[0 : len(password)-1]
	return password
}

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}
