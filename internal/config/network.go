package config

import "regexp"

var networkInterfaceID = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.:@-]{0,63}$`)

// ValidNetworkInterface permits reporting identifiers for physical and virtual
// interfaces. The empty identifier selects automatic mode; paths are rejected.
func ValidNetworkInterface(value string) bool {
	return value == "" || networkInterfaceID.MatchString(value)
}
