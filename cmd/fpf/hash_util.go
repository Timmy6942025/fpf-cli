package main

import (
	"fmt"
	"hash/crc32"
)

func stableChecksum(input string) string {
	return fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(input)))
}
