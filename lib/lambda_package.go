package lib

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

var lambdaPackageModifiedTime = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

func normalizeLambdaPackage(zipFile string) error {
	reader, err := zip.OpenReader(zipFile)
	if err != nil {
		return err
	}
	readerOpen := true
	defer func() {
		if readerOpen {
			_ = reader.Close()
		}
	}()

	entries := append([]*zip.File(nil), reader.File...)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	info, err := os.Stat(zipFile)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(zipFile), ".lambda-package-*.zip")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	temporaryOpen := true
	defer func() {
		if temporaryOpen {
			_ = temporary.Close()
		}
		if temporaryName != "" {
			_ = os.Remove(temporaryName)
		}
	}()

	writer := zip.NewWriter(temporary)
	writerOpen := true
	defer func() {
		if writerOpen {
			_ = writer.Close()
		}
	}()
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if _, ok := seen[entry.Name]; ok {
			return fmt.Errorf("lambda package contains duplicate path: %s", entry.Name)
		}
		seen[entry.Name] = struct{}{}

		content, err := entry.Open()
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(io.Discard, content)
		closeErr := content.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}

		mode := entry.Mode()
		switch {
		case mode.IsRegular():
			if mode.Perm()&0o111 != 0 {
				mode = 0o755
			} else {
				mode = 0o644
			}
		case mode.IsDir():
			mode = os.ModeDir | 0o755
		case mode&os.ModeSymlink != 0:
			mode = os.ModeSymlink | 0o777
		default:
			return fmt.Errorf("lambda package contains unsupported file mode for: %s", entry.Name)
		}
		header := entry.FileHeader
		header.SetMode(mode)
		header.SetModTime(lambdaPackageModifiedTime)
		header.Extra = nil
		header.Comment = ""
		destination, err := writer.CreateRaw(&header)
		if err != nil {
			return err
		}
		source, err := entry.OpenRaw()
		if err != nil {
			return err
		}
		if _, err := io.Copy(destination, source); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	writerOpen = false
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	temporaryOpen = false
	if err := reader.Close(); err != nil {
		return err
	}
	readerOpen = false
	if err := os.Rename(temporaryName, zipFile); err != nil {
		return err
	}
	temporaryName = ""
	return nil
}
