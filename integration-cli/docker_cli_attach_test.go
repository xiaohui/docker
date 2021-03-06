package main

import (
	"bufio"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/go-check/check"
)

const attachWait = 5 * time.Second

func (s *DockerSuite) TestAttachMultipleAndRestart(c *check.C) {

	endGroup := &sync.WaitGroup{}
	startGroup := &sync.WaitGroup{}
	endGroup.Add(3)
	startGroup.Add(3)

	if err := waitForContainer("attacher", "-d", "busybox", "/bin/sh", "-c", "while true; do sleep 1; echo hello; done"); err != nil {
		c.Fatal(err)
	}

	startDone := make(chan struct{})
	endDone := make(chan struct{})

	go func() {
		endGroup.Wait()
		close(endDone)
	}()

	go func() {
		startGroup.Wait()
		close(startDone)
	}()

	for i := 0; i < 3; i++ {
		go func() {
			cmd := exec.Command(dockerBinary, "attach", "attacher")

			defer func() {
				cmd.Wait()
				endGroup.Done()
			}()

			out, err := cmd.StdoutPipe()
			if err != nil {
				c.Fatal(err)
			}

			if err := cmd.Start(); err != nil {
				c.Fatal(err)
			}

			buf := make([]byte, 1024)

			if _, err := out.Read(buf); err != nil && err != io.EOF {
				c.Fatal(err)
			}

			startGroup.Done()

			if !strings.Contains(string(buf), "hello") {
				c.Fatalf("unexpected output %s expected hello\n", string(buf))
			}
		}()
	}

	select {
	case <-startDone:
	case <-time.After(attachWait):
		c.Fatalf("Attaches did not initialize properly")
	}

	cmd := exec.Command(dockerBinary, "kill", "attacher")
	if _, err := runCommand(cmd); err != nil {
		c.Fatal(err)
	}

	select {
	case <-endDone:
	case <-time.After(attachWait):
		c.Fatalf("Attaches did not finish properly")
	}

}

func (s *DockerSuite) TestAttachTtyWithoutStdin(c *check.C) {

	cmd := exec.Command(dockerBinary, "run", "-d", "-ti", "busybox")
	out, _, err := runCommandWithOutput(cmd)
	if err != nil {
		c.Fatalf("failed to start container: %v (%v)", out, err)
	}

	id := strings.TrimSpace(out)
	if err := waitRun(id); err != nil {
		c.Fatal(err)
	}

	defer func() {
		cmd := exec.Command(dockerBinary, "kill", id)
		if out, _, err := runCommandWithOutput(cmd); err != nil {
			c.Fatalf("failed to kill container: %v (%v)", out, err)
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)

		cmd := exec.Command(dockerBinary, "attach", id)
		if _, err := cmd.StdinPipe(); err != nil {
			c.Fatal(err)
		}

		expected := "cannot enable tty mode"
		if out, _, err := runCommandWithOutput(cmd); err == nil {
			c.Fatal("attach should have failed")
		} else if !strings.Contains(out, expected) {
			c.Fatalf("attach failed with error %q: expected %q", out, expected)
		}
	}()

	select {
	case <-done:
	case <-time.After(attachWait):
		c.Fatal("attach is running but should have failed")
	}

}

func (s *DockerSuite) TestAttachDisconnect(c *check.C) {
	out, _ := dockerCmd(c, "run", "-di", "busybox", "/bin/cat")
	id := strings.TrimSpace(out)

	cmd := exec.Command(dockerBinary, "attach", id)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		c.Fatal(err)
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		c.Fatal(err)
	}
	defer stdout.Close()
	if err := cmd.Start(); err != nil {
		c.Fatal(err)
	}
	defer cmd.Process.Kill()

	if _, err := stdin.Write([]byte("hello\n")); err != nil {
		c.Fatal(err)
	}
	out, err = bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		c.Fatal(err)
	}
	if strings.TrimSpace(out) != "hello" {
		c.Fatalf("exepected 'hello', got %q", out)
	}

	if err := stdin.Close(); err != nil {
		c.Fatal(err)
	}

	// Expect container to still be running after stdin is closed
	running, err := inspectField(id, "State.Running")
	if err != nil {
		c.Fatal(err)
	}
	if running != "true" {
		c.Fatal("exepected container to still be running")
	}

}
