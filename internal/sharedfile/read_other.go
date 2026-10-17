//go:build !windows

package sharedfile

import "os"

func read(path string) ([]byte, error) { return os.ReadFile(path) }
