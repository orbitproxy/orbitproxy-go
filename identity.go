package orbitproxy

// Identity is the runtime input for Start.
// SDK Register fills these fields; CLI builds the same shape from its own
// register + local config (machine_key, edge_addr, private key).
type Identity struct {
	MachineKey     string
	EdgeAddr      string
	MachineCACert string // PEM of OrbitProxy Machine CA (from register)
	PrivateKeyPEM string
}
