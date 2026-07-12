package http

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// UploadedFile wraps a single multipart file upload, analogous to
// Laravel's Illuminate\Http\UploadedFile. Every UploadedFile returned by
// Context.File is backed by a real temporary file on disk — Go's
// multipart parser only spills large uploads to disk, keeping small ones
// in memory, but Path() needs to be reliable unconditionally (matching
// PHP, which always spools uploads to a temp file), so Context.File always
// copies the upload out to one via os.CreateTemp.
type UploadedFile struct {
	// Filename is the original, client-submitted filename. It's untrusted
	// input — never used directly as a filesystem path; see Store/StoreAs.
	Filename string
	// Size is the upload's size in bytes.
	Size int64

	tempPath string
}

// IsValid reports whether the file was received without error and has a
// backing temporary file to read from.
func (f *UploadedFile) IsValid() bool {
	return f != nil && f.tempPath != ""
}

// Path returns the temporary file path Golite copied the upload to for
// the duration of this request. The file is removed automatically once
// the request finishes (see Kernel.ServeHTTP) unless Store/StoreAs already
// moved it elsewhere.
func (f *UploadedFile) Path() string {
	return f.tempPath
}

// Extension guesses the file's extension from its actual content — via
// net/http.DetectContentType, which sniffs the first 512 bytes — rather
// than trusting the client-submitted filename or Content-Type, both of
// which are attacker-controlled. Falls back to the submitted filename's
// extension if the content can't be identified.
func (f *UploadedFile) Extension() string {
	if !f.IsValid() {
		return ""
	}

	file, err := os.Open(f.tempPath)
	if err != nil {
		return strings.TrimPrefix(filepath.Ext(f.Filename), ".")
	}
	defer file.Close()

	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	detected := http.DetectContentType(buf[:n])

	// DetectContentType appends parameters for some types (e.g.
	// "text/plain; charset=utf-8"); ExtensionsByType wants just the type.
	mediaType, _, err := mime.ParseMediaType(detected)
	if err != nil {
		mediaType = detected
	}

	exts, err := mime.ExtensionsByType(mediaType)
	if err != nil || len(exts) == 0 {
		return strings.TrimPrefix(filepath.Ext(f.Filename), ".")
	}

	// ExtensionsByType can return several candidates for one MIME type
	// (e.g. both ".jpg" and ".jpeg"); prefer the shortest, most common form.
	best := exts[0]
	for _, e := range exts[1:] {
		if len(e) < len(best) {
			best = e
		}
	}
	return strings.TrimPrefix(best, ".")
}

// Store saves the upload into destinationDir under an automatically
// generated, collision-resistant filename (a random hex name plus the
// sniffed extension), returning the path it was written to.
func (f *UploadedFile) Store(destinationDir string) (string, error) {
	return f.StoreAs(destinationDir, f.generateUniqueFilename())
}

func (f *UploadedFile) generateUniqueFilename() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	name := hex.EncodeToString(buf)
	if ext := f.Extension(); ext != "" {
		name += "." + ext
	}
	return name
}

// StoreAs saves the upload into destinationDir under the given filename —
// deliberately not the client-submitted one, which is untrusted input a
// caller would need to sanitize (path traversal, reserved device names,
// ...) before it's ever safe to use as part of a filesystem path; prefer
// Store, which generates a safe one automatically. StoreAs creates
// destinationDir if needed and returns the resulting path.
//
// It moves the file (os.Rename) when possible — the common case, since
// the temp file and a destination under the app's own storage directory
// are normally on the same filesystem — falling back to a copy-then-remove
// when Rename fails (e.g. destinationDir is on a different device).
func (f *UploadedFile) StoreAs(destinationDir, filename string) (string, error) {
	if !f.IsValid() {
		return "", errors.New("golite: cannot store an invalid uploaded file")
	}
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		return "", err
	}

	destPath := filepath.Join(destinationDir, filename)

	if err := os.Rename(f.tempPath, destPath); err == nil {
		f.tempPath = destPath
		return destPath, nil
	}

	src, err := os.Open(f.tempPath)
	if err != nil {
		return "", err
	}
	defer src.Close()

	dst, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", err
	}

	_ = os.Remove(f.tempPath)
	f.tempPath = destPath
	return destPath, nil
}
