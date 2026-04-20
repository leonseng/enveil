package agent

import "encoding/json"

// Op is the operation type in a client request.
type Op string

const (
	OpResolve = Op("resolve")
	OpList    = Op("list")
	OpAdd     = Op("add")
	OpDelete  = Op("delete")
	OpRotate  = Op("rotate")
)

// Request is sent by the client to the agent.
type Request struct {
	Op    Op     `json:"op"`
	Ref   string `json:"ref,omitempty"`
	Item  string `json:"item,omitempty"`
	Field string `json:"field,omitempty"`
	Value string `json:"value,omitempty"`
}

// Response is sent by the agent back to the client.
type Response struct {
	Value string   `json:"value,omitempty"`
	Keys  []string `json:"keys,omitempty"`
	Error string   `json:"error,omitempty"`
}

// encodeRequest serialises a request to newline-delimited JSON.
func encodeRequest(r Request) ([]byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// encodeResponse serialises a response to newline-delimited JSON.
func encodeResponse(r Response) ([]byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
