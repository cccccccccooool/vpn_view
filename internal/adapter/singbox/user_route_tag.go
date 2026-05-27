package singbox

import (
	"encoding/hex"
	"strings"
)

const userRouteTagPrefix = "vpnview-user-"

func userRouteTag(userID string) string {
	return userRouteTagPrefix + hex.EncodeToString([]byte(userID))
}

func userIDFromRouteTag(tag string) string {
	encoded, ok := strings.CutPrefix(tag, userRouteTagPrefix)
	if !ok || encoded == "" {
		return ""
	}
	raw, err := hex.DecodeString(encoded)
	if err != nil {
		return ""
	}
	return string(raw)
}

func isUserRouteTag(tag string) bool {
	return strings.HasPrefix(tag, userRouteTagPrefix)
}
