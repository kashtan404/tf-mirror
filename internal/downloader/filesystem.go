package downloader

import (
	"io"
	"os"
)

// File system operation wrappers for easier testing

func createDirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func createFileHandle(path string) (io.WriteCloser, error) {
	return os.Create(path)
}

func removeFileHandle(path string) error {
	return os.Remove(path)
}

func renameFileHandle(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func readDir(path string) ([]os.DirEntry, error) {
	return os.ReadDir(path)
}

func statFile(path string) (os.FileInfo, error) {
	return os.Stat(path)
}
