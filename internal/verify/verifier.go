package verify

// Verifier checks whether the peer process (identified by PID) is permitted
// to communicate with the agent.
type Verifier interface {
	Verify(pid uint32) (bool, error)
}

// Noop is a Verifier that always approves. Use only in tests.
type Noop struct{}

func (Noop) Verify(_ uint32) (bool, error) { return true, nil }
