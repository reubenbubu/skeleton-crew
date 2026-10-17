// Package sharedfile reads small files that another crew process may
// replace or remove at the same moment.
package sharedfile

// Read returns the contents of the file at path, like os.ReadFile. On
// Windows it also lets another process remove or replace the file while it
// is being read, as Linux and macOS always do.
func Read(path string) ([]byte, error) { return read(path) }
