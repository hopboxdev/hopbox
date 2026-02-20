package helper

const (
	ActionConfigureTUN = "configure_tun"
	ActionCleanupTUN   = "cleanup_tun"
	ActionAddHost      = "add_host"
	ActionRemoveHost   = "remove_host"
)

// Request is sent from hop to the helper daemon over the Unix socket.
type Request struct {
	Action    string `json:"action"`
	Interface string `json:"interface,omitempty"`
	LocalIP   string `json:"local_ip,omitempty"`
	PeerIP    string `json:"peer_ip,omitempty"`
	IP        string `json:"ip,omitempty"`
	Hostname  string `json:"hostname,omitempty"`
}

// Response is sent back from the helper daemon.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// SocketPath is where the helper daemon listens.
const SocketPath = "/var/run/hopbox/helper.sock"
