package lib

import (
	"archive/zip"
	"compress/flate"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

var lambdaPackageModifiedTime = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

func lambdaPackageRoot() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("libaws-lambda-%d", os.Geteuid()))
}

func LambdaZipFile(name string) string {
	return filepath.Join(lambdaPackageRoot(), sha256Hex([]byte(name)), "lambda.zip")
}

func ensureLambdaPackageRoot() error {
	root := lambdaPackageRoot()
	if err := os.Mkdir(root, 0o700); err != nil && !os.IsExist(err) {
		return fmt.Errorf("create lambda package root %s: %w", root, err)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("inspect lambda package root %s: %w", root, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("lambda package root is not a directory: %s", root)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("lambda package root is not owned by the current user: %s", root)
	}
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("lambda package root permissions must be 0700: %s", root)
	}
	return nil
}

func resetLambdaPackageDir(name string) (string, error) {
	if err := validateLambdaName(name); err != nil {
		return "", err
	}
	if err := ensureLambdaPackageRoot(); err != nil {
		return "", err
	}
	dir := filepath.Dir(LambdaZipFile(name))
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

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
	writer.RegisterCompressor(zip.Deflate, func(destination io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(destination, flate.BestCompression)
	})
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
		header := &zip.FileHeader{Name: entry.Name, Method: zip.Deflate, Modified: lambdaPackageModifiedTime}
		header.SetMode(mode)
		destination, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		content, err := entry.Open()
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(destination, content)
		closeErr := content.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
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
