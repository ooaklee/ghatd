//go:build !windows

package config

import (
	"fmt"
	"os"
)

// getFilePermission accepts only owner-only 0400 or 0600 config files,
// returning an error naming the required modes otherwise.
func getFilePermission(fi os.FileInfo) error {
	if fi.Mode().Perm() == 0600 || fi.Mode().Perm() == 0400 {
		return nil
	}
	return fmt.Errorf("config file has incorrect permission flags:%s."+
		"change the file permission either to 0400 or 0600.", fi.Mode().Perm().String())
}
