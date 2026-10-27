package update

// This file writes "Android sparse" images, the format rpi-image-gen uses
// for the boot and system partitions in an update bundle. A sparse image
// leaves out the empty parts of a partition, so it is much smaller than
// the partition itself.
//
// Format (all numbers little-endian):
//
//	file header (28 bytes): magic 0xED26FF3A, version 1.0, header sizes,
//	                        block size, total blocks, number of chunks
//	then chunks, each with a 12-byte header:
//	  RAW        (0xCAC1): the next N blocks of data follow
//	  FILL       (0xCAC2): N blocks filled with a repeated 4-byte value
//	  DONT_CARE  (0xCAC3): skip N blocks (unused space)
//	  CRC32      (0xCAC4): a checksum, which we skip (the whole bundle is
//	                       already checked against the signed SHA-256)

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	sparseMagic    = 0xED26FF3A
	chunkRaw       = 0xCAC1
	chunkFill      = 0xCAC2
	chunkDontCare  = 0xCAC3
	chunkCRC32     = 0xCAC4
	fileHeaderSize = 28
	chunkHeadSize  = 12
	// maxBlockSize is the largest block size accepted. rpi-image-gen uses
	// 4096 bytes. The image is read before its checksum can be checked,
	// so a broken or hostile one must not make rbd set aside gigabytes
	// for a single block.
	maxBlockSize = 1 << 20
)

// sparseHeader is the start of a sparse image.
type sparseHeader struct {
	Magic          uint32
	MajorVersion   uint16
	MinorVersion   uint16
	FileHeaderSize uint16
	ChunkHeadSize  uint16
	BlockSize      uint32
	TotalBlocks    uint32
	TotalChunks    uint32
	ImageChecksum  uint32
}

type chunkHeader struct {
	Type      uint16
	Reserved  uint16
	Blocks    uint32 // size of the chunk in the output, in blocks
	TotalSize uint32 // size of the chunk in the input, header included
}

// WriteSparse expands the sparse image read from src into dst (for
// example a partition), and returns the full size of the image in bytes.
// maxSize is the size of dst; an image that doesn't fit is refused before
// anything is written.
func WriteSparse(dst io.WriterAt, src io.Reader, maxSize int64) (int64, error) {
	var h sparseHeader
	if err := binary.Read(src, binary.LittleEndian, &h); err != nil {
		return 0, fmt.Errorf("reading sparse header: %w", err)
	}
	if h.Magic != sparseMagic || h.MajorVersion != 1 || h.FileHeaderSize < fileHeaderSize ||
		h.ChunkHeadSize < chunkHeadSize || h.BlockSize == 0 || h.BlockSize%4 != 0 {
		return 0, errors.New("not a sparse image")
	}
	if h.BlockSize > maxBlockSize {
		return 0, fmt.Errorf("its block size (%d bytes) is larger than %d bytes", h.BlockSize, maxBlockSize)
	}
	// Skip any extra header bytes from a newer format version.
	if _, err := io.CopyN(io.Discard, src, int64(h.FileHeaderSize-fileHeaderSize)); err != nil {
		return 0, err
	}
	blockSize := int64(h.BlockSize)
	imageSize := int64(h.TotalBlocks) * blockSize
	if imageSize > maxSize {
		return 0, fmt.Errorf("the image (%d bytes) is bigger than the partition (%d bytes)", imageSize, maxSize)
	}

	buffer := make([]byte, blockSize)
	offset := int64(0)
	for i := uint32(0); i < h.TotalChunks; i++ {
		var c chunkHeader
		if err := binary.Read(src, binary.LittleEndian, &c); err != nil {
			return 0, fmt.Errorf("reading chunk %d: %w", i, err)
		}
		if _, err := io.CopyN(io.Discard, src, int64(h.ChunkHeadSize-chunkHeadSize)); err != nil {
			return 0, err
		}
		length := int64(c.Blocks) * blockSize
		if offset+length > imageSize {
			return 0, fmt.Errorf("chunk %d goes past the end of the image", i)
		}

		switch c.Type {
		case chunkRaw:
			if int64(c.TotalSize) != int64(h.ChunkHeadSize)+length {
				return 0, fmt.Errorf("chunk %d has the wrong size", i)
			}
			if _, err := io.Copy(io.NewOffsetWriter(dst, offset), io.LimitReader(src, length)); err != nil {
				return 0, fmt.Errorf("writing chunk %d: %w", i, err)
			}
		case chunkFill:
			var pattern [4]byte
			if _, err := io.ReadFull(src, pattern[:]); err != nil {
				return 0, err
			}
			for j := 0; j < len(buffer); j += 4 {
				copy(buffer[j:], pattern[:])
			}
			for b := int64(0); b < int64(c.Blocks); b++ {
				if _, err := dst.WriteAt(buffer, offset+b*blockSize); err != nil {
					return 0, fmt.Errorf("writing chunk %d: %w", i, err)
				}
			}
		case chunkDontCare:
			// Unused space: leave it as it is.
		case chunkCRC32:
			if _, err := io.CopyN(io.Discard, src, 4); err != nil {
				return 0, err
			}
			length = 0
		default:
			return 0, fmt.Errorf("chunk %d has an unknown type %#x", i, c.Type)
		}
		offset += length
	}
	if offset != imageSize {
		return 0, fmt.Errorf("the chunks cover %d bytes, but the image is %d bytes", offset, imageSize)
	}
	return imageSize, nil
}
