package control

// Request is sent from the in-container CLI to hopboxd via the Unix socket.
type Request struct {
	Command string `json:"command"`           // "status" | "destroy"
	Confirm string `json:"confirm,omitempty"` // box name confirmation for destroy
}

// Response is sent from hopboxd back to the CLI.
type Response struct {
	OK    bool              `json:"ok"`
	Data  map[string]string `json:"data,omitempty"`
	Error string            `json:"error,omitempty"`
}
