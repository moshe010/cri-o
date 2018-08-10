package compression

import (
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"io"
	"io/ioutil"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/ulikunitz/xz"
)

// DecompressorFunc returns the decompressed stream, given a compressed stream.
// The caller must call Close() on the decompressed stream (even if the compressed input stream does not need closing!).
type DecompressorFunc func(io.Reader) (io.ReadCloser, error)

// GzipDecompressor is a DecompressorFunc for the gzip compression algorithm.
func GzipDecompressor(r io.Reader) (io.ReadCloser, error) {
	return gzip.NewReader(r)
}

// Bzip2Decompressor is a DecompressorFunc for the bzip2 compression algorithm.
func Bzip2Decompressor(r io.Reader) (io.ReadCloser, error) {
	return ioutil.NopCloser(bzip2.NewReader(r)), nil
}

// XzDecompressor is a DecompressorFunc for the xz compression algorithm.
func XzDecompressor(r io.Reader) (io.ReadCloser, error) {
	r, err := xz.NewReader(r)
	if err != nil {
		return nil, err
	}
	return ioutil.NopCloser(r), nil
}

// compressionAlgos is an internal implementation detail of DetectCompression
var compressionAlgos = map[string]struct {
	prefix       []byte
	decompressor DecompressorFunc
}{
	"gzip":  {[]byte{0x1F, 0x8B, 0x08}, GzipDecompressor},                 // gzip (RFC 1952)
	"bzip2": {[]byte{0x42, 0x5A, 0x68}, Bzip2Decompressor},                // bzip2 (decompress.c:BZ2_decompress)
	"xz":    {[]byte{0xFD, 0x37, 0x7A, 0x58, 0x5A, 0x00}, XzDecompressor}, // xz (/usr/share/doc/xz/xz-file-format.txt)
}

// DetectCompression returns a DecompressorFunc if the input is recognized as a compressed format, nil otherwise.
// Because it consumes the start of input, other consumers must use the returned io.Reader instead to also read from the beginning.
func DetectCompression(input io.Reader) (DecompressorFunc, io.Reader, error) {
	buffer := [8]byte{}

	n, err := io.ReadAtLeast(input, buffer[:], len(buffer))
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		// This is a “real” error. We could just ignore it this time, process the data we have, and hope that the source will report the same error again.
		// Instead, fail immediately with the original error cause instead of a possibly secondary/misleading error returned later.
		return nil, nil, err
	}

	var decompressor DecompressorFunc
	for name, algo := range compressionAlgos {
		if bytes.HasPrefix(buffer[:n], algo.prefix) {
			logrus.Debugf("Detected compression format %s", name)
			decompressor = algo.decompressor
			break
		}
	}
	if decompressor == nil {
		logrus.Debugf("No compression detected")
	}

	return decompressor, io.MultiReader(bytes.NewReader(buffer[:n]), input), nil
}

// AutoDecompress takes a stream and returns an uncompressed version of the
// same stream.
// The caller must call Close() on the returned stream (even if the input does not need,
// or does not even support, closing!).
func AutoDecompress(stream io.Reader) (io.ReadCloser, bool, error) {
	decompressor, stream, err := DetectCompression(stream)
	if err != nil {
		return nil, false, errors.Wrapf(err, "Error detecting compression")
	}
	var res io.ReadCloser
	if decompressor != nil {
		res, err = decompressor(stream)
		if err != nil {
			return nil, false, errors.Wrapf(err, "Error initializing decompression")
		}
	} else {
		res = ioutil.NopCloser(stream)
	}
	return res, decompressor != nil, nil
}
