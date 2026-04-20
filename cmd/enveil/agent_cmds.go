package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/leonzalion/enveil/internal/agent"
	"github.com/leonzalion/enveil/internal/store"
	"github.com/leonzalion/enveil/internal/verify"
)

// agentStart forks the agent to the background and prints eval-able exports.
func agentStart() error {
	// Check if an agent is already running.
	if agentIsRunning() {
		return fmt.Errorf("agent is already running — use: enveil agent status")
	}

	password, err := promptPassword("Master password: ")
	if err != nil {
		return err
	}

	sp := storePath()
	if _, err := os.Stat(sp); os.IsNotExist(err) {
		return fmt.Errorf("store not initialised — run: enveil init")
	}

	// Validate the password before forking.
	if _, err := store.Open(sp, password); err != nil {
		return fmt.Errorf("wrong master password: %w", err)
	}

	// Fork self as the background agent process.
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("os.Executable: %w", err)
	}

	// Write the password to a pipe that the child will read from stdin.
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}

	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(), agentInternalFlag+"=1")
	cmd.Stdin = r
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		r.Close()
		w.Close()
		return fmt.Errorf("starting agent: %w", err)
	}

	// Send password to child then close our end.
	w.Write(password)
	w.Close()
	r.Close()

	// Wait briefly for the agent to write its env file.
	envFile := agent.EnvFilePath()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(envFile); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	data, err := os.ReadFile(envFile)
	if err != nil {
		return fmt.Errorf("agent did not start in time: %w", err)
	}

	// Print export lines to stdout for eval.
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			fmt.Println(line)
		}
	}
	return nil
}

// agentStop sends SIGTERM to the agent via its PID in the env file.
func agentStop() error {
	pid, err := readAgentPID()
	if err != nil {
		return fmt.Errorf("agent not running: %w", err)
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("finding process %d: %w", pid, err)
	}
	if err := p.Signal(os.Interrupt); err != nil {
		return fmt.Errorf("signalling agent: %w", err)
	}
	fmt.Fprintln(os.Stderr, "Agent stopped.")
	return nil
}

// agentStatus checks if the agent socket is reachable.
func agentStatus() error {
	if !agentIsRunning() {
		fmt.Fprintln(os.Stderr, "Agent is not running.")
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "Agent is running.")
	return nil
}

func agentIsRunning() bool {
	sockPath := os.Getenv("ENVEIL_AUTH_SOCK")
	if sockPath == "" {
		sockPath = agent.SocketPath(os.Getuid())
	}
	conn, err := net.DialTimeout("unix", sockPath, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func readAgentPID() (int, error) {
	data, err := os.ReadFile(agent.EnvFilePath())
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "export ENVEIL_AGENT_PID=") {
			pidStr := strings.TrimPrefix(line, "export ENVEIL_AGENT_PID=")
			return strconv.Atoi(strings.TrimSpace(pidStr))
		}
	}
	return 0, fmt.Errorf("PID not found in env file")
}

// runAgentInternal is called when the process is the re-exec'd background agent.
// It reads the master password from stdin, then serves the socket loop.
func runAgentInternal() {
	// Read password from stdin.
	buf := make([]byte, 1024)
	n, err := os.Stdin.Read(buf)
	if err != nil || n == 0 {
		os.Exit(1)
	}
	password := buf[:n]

	sp := storePath()
	s, err := store.Open(sp, password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "enveil agent: %v\n", err)
		os.Exit(1)
	}

	var verifier verify.Verifier
	v, err := verify.NewInodeVerifier()
	if err != nil {
		fmt.Fprintf(os.Stderr, "enveil agent: verifier init: %v\n", err)
		os.Exit(1)
	}
	verifier = v

	a := agent.New(s, verifier)
	if err := a.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "enveil agent: %v\n", err)
		os.Exit(1)
	}
}

// lookPath finds the full path of a binary using PATH.
func lookPath(name string) (string, error) {
	return exec.LookPath(name)
}
