package expect_test

import (
	"io"
	"os/exec"
	"testing"
	"time"

	"reflect"
	"github.com/jamesharr/expect"
	"github.com/kr/pty"
)

func TestExpect_timeout(t *testing.T) {
	// Start basic
	t.Log("Starting Command")
	cmd := exec.Command("bash", "-c", "sleep 0.1; echo hello")
	p, err := pty.Start(cmd)
	if err != nil {
		t.Error("Start failed:", err)
	}
	exp := expect.Create(p, func() {})
	defer exp.Close()
	exp.SetLogger(expect.TestLogger(t))

	// This should timeout
	t.Log("Expect - should timeout")
	exp.SetTimeout(time.Millisecond)
	m, err := exp.Expect("[Hh]ello")
	t.Logf(" err=%#v", err)
	if err != expect.ErrTimeout {
		t.Error("Expecting timeout, but got", err)
	}
	if m.Before != "" {
		t.Errorf("m.Before should be empty, but got %q", m.Before)
	}
	if !reflect.DeepEqual(m.Groups, []string(nil)) {
		t.Errorf("Expecting m.Groups to be empty, got %q", m.Groups)
	}

	// Try to get get the final text
	t.Log("Test - should finish immediately")
	t.Logf(" buffer[pre]:%#v", exp.Buffer())
	exp.SetTimeout(time.Second)
	m, err = exp.Expect("e(l+)o")
	t.Logf(" m=%#v, err=%#v", m, err)
	if err != nil {
		t.Error("Expecting error to be nil, but got", err)
	}
	m_exp := expect.Match{
		Before: "h",
		Groups: []string{"ello", "ll"},
	}
	if !reflect.DeepEqual(m, m_exp) {
		t.Errorf("Expecting match to be %v, but got %v", m, m_exp)
	}

	// Test assert
	t.Log("Test should return an EOF")
	//	t.Logf(" Buffer: %#v", exp.Buffer())
	err = exp.ExpectEOF()
	t.Logf(" err=%#v", err)
	if err != io.EOF {
		t.Error("Expecting EOF error, got", err)
	}
}

func TestExpect_send(t *testing.T) {
	// Start cat
	exp, err := expect.Spawn("cat")
	if err != nil {
		t.Error("Unexpected error spawning 'cat'", err)
	}
	defer exp.Close()
	exp.SetLogger(expect.TestLogger(t))
	exp.SetTimeout(time.Second)

	// Send some data
	err = exp.Send("Hello\nWorld\n")
	if err != nil {
		t.Error("Unexpected error spawning 'cat'", err)
	}

	// Get first chunk
	m, err := exp.Expect("Hello")
	if err != nil {
		t.Error("Expect() error:", err)
	}
	m_exp := expect.Match{
		Before: "",
		Groups: []string{"Hello"},
	}
	if !reflect.DeepEqual(m, m_exp) {
		t.Errorf("expected match to be %v, got %v", m_exp, m)
	}

	// Check new lines
	m, err = exp.Expect("World\n")
	m_exp = expect.Match{
		Before: "\n",
		Groups: []string{"World\n"},
	}
	if !reflect.DeepEqual(m, m_exp) {
		t.Errorf("expected match to be %v, got %v", m_exp, m)
	}
}

func TestExpect_largeBuffer(t *testing.T) {
	// Start cat
	exp, err := expect.Spawn("cat")
	if err != nil {
		t.Error("Unexpected error spawning 'cat'", err)
	}
	defer exp.Close()
	exp.SetLogger(expect.TestLogger(t))
	exp.SetTimeout(time.Second)

	// Sending large amounts of text
	t.Log("Generating large amounts of text")
	text := make([]byte, 128)
	for i := range text {
		text[i] = '.'
	}
	text[len(text)-1] = '\n'

	t.Log("Writing large amounts of text")
	for i := 0; i < 1024; i++ {
		//		t.Logf(" Writing %d bytes", i*len(text))
		err := exp.Send(string(text))
		if err != nil {
			t.Logf(" Send Error: %#v", err)
		}
	}
	exp.Send("\nDONE\n")

	t.Log("Expecting to see finish message")
	match, err := exp.Expect("DONE")
	t.Logf(" match.Groups=%#v", match.Groups)
	t.Logf(" err=%#v", err)
	if err != nil {
		t.Error("Unexpected err", err)
	}

}
