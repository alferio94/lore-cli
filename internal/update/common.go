package update

import (
	"crypto/sha256"
	"encoding/hex"
)

const unixArchiveBinaryName = "lore"

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
