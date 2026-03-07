package lha

import (
	"fmt"
	"os"
	"time"
)

type headerReader func(r *Reader) (*Header, error)

var headerReaders = map[byte]headerReader{
	0: readHeaderLv0,
	1: readHeaderLv1,
	2: readHeaderLv2,
	3: readHeaderLv3,
}

func readHeaderLv0(r *Reader) (*Header, error) {
	if r.err != nil {
		return nil, r.err
	}
	h := new(Header)
	size, _ := r.readUint8()
	h.Size = uint16(size)
	_, _ = r.readUint8()
	// hhcs := uint16(hcs)
	// h.HeaderCRC = &hhcs
	h.Method, _ = r.readStringN(5)
	packedSize, _ := r.readUint32()
	h.PackedSize = uint64(packedSize)
	originalSize, _ := r.readUint32()
	h.OriginalSize = uint64(originalSize)

	// Conver DOS Time Format to time.Time: https://msdn.microsoft.com/ja-jp/library/cc429703.aspx
	date, _ := r.readUint16()
	day := int(date & 0x1f)
	mon := time.Month(date & 0x1ef >> 5)
	year := int(date&0xfe00>>9 + 1980)
	t, _ := r.readUint16()
	sec := int(t & 0x1f * 2)
	min := int(t & 0x7e0 >> 5)
	hour := int(t & 0xf800 >> 11)
	h.Time = time.Date(year, mon, day, hour, min, sec, 0, time.Local)
	h.Attribute, _ = r.readUint8()
	h.Level, _ = r.readUint8()
	flen, _ := r.readUint8()
	h.Name, _ = r.readStringN(int(flen))
	*(*uint16)(&h.CRC), _ = r.readUint16()

	// FIXME: consider 64bit length.
	if remain := int(h.Size+2) - int(r.cnt); remain > 0 {
		r.skip(remain)
	}
	if r.err != nil {
		return nil, r.err
	}
	return h, nil
}

func readHeaderLv1(r *Reader) (*Header, error) {
	return nil, fmt.Errorf("unsupported header level: %d", 1)
}

func readHeaderLv2(r *Reader) (*Header, error) {
	if r.err != nil {
		return nil, r.err
	}
	h := new(Header)
	h.Size, _ = r.readUint16()
	h.Method, _ = r.readStringN(5)
	packedSize, _ := r.readUint32()
	h.PackedSize = uint64(packedSize)
	originalSize, _ := r.readUint32()
	h.OriginalSize = uint64(originalSize)
	h.Time, _ = r.readTime()
	h.Attribute, _ = r.readUint8()
	h.Level, _ = r.readUint8()
	*(*uint16)(&h.CRC), _ = r.readUint16()
	h.OSID, _ = r.readUint8()
	nextSize, _ := r.readUint16()
	readAllExtendedHeaders(r, h, nextSize)
	// FIXME: consider 64bit length.
	if remain := int(h.Size) - int(r.cnt); remain > 0 {
		r.skip(remain)
	}
	if r.err != nil {
		return nil, r.err
	}
	return h, nil
}

func readHeaderLv3(r *Reader) (*Header, error) {
	return nil, fmt.Errorf("unsupported header level: %d", 3)
}

type exHeaderReader func(r *Reader, h *Header, size int) (remain int, err error)

var exHeaderReaders = map[uint8]exHeaderReader{
	0x00: readHeaderCRC,
	0x01: readFilename,
	0x02: readDirectory,
	0x40: readDOSAttr,
	0x41: readWinTime,
	0x42: readWinSize,
	0x50: readUNIXPerm,
	0x51: readUNIXGIDUID,
	0x52: readUNIXGroup,
	0x53: readUNIXUser,
	0x54: readUNIXTime,
}

func readAllExtendedHeaders(r *Reader, h *Header, size uint16) error {
	if r.err != nil {
		return r.err
	}
	for size > 0 {
		if size < 3 {
			r.err = errTooShortExtendedHeader
			return r.err
		}
		size, r.err = readExtendedHeader(r, h, size)
		if r.err != nil {
			return r.err
		}
	}
	return nil
}

func readExtendedHeader(r *Reader, h *Header, size uint16) (uint16, error) {
	t, err := r.readUint8()
	if err != nil {
		return 0, err
	}
	proc, ok := exHeaderReaders[t]
	remain := int(size) - 3
	if ok {
		remain, err = proc(r, h, remain)
		if err != nil {
			return 0, err
		}
	}
	if remain > 0 {
		r.skip(remain)
	}
	return r.readUint16()
}

func readHeaderCRC(r *Reader, h *Header, size int) (remain int, err error) {
	crc := new(uint16)
	*crc, err = r.readUint16NoCRC()
	if err == nil {
		h.HeaderCRC = crc
	}
	return remain - 2, err
}

func readFilename(r *Reader, h *Header, size int) (remain int, err error) {
	h.Name, err = r.readStringN(size)
	return 0, err
}

func readDirectory(r *Reader, h *Header, size int) (remain int, err error) {
	d, err := r.readBytes(size)
	for i := range d {
		if d[i] == 0xff {
			d[i] = os.PathSeparator
		}
	}
	h.Dir = string(d)
	return 0, err
}

func readDOSAttr(r *Reader, h *Header, size int) (remain int, err error) {
	h.DOS.Attr, err = r.readUint16()
	return size - 2, err
}

func readWinTime(r *Reader, h *Header, size int) (remain int, err error) {
	// FIXME: parse Windows time.
	return size, r.err
}

func readWinSize(r *Reader, h *Header, size int) (remain int, err error) {
	h.PackedSize, _ = r.readUint64()
	h.OriginalSize, err = r.readUint64()
	return size - 16, err
}

func readUNIXPerm(r *Reader, h *Header, size int) (remain int, err error) {
	h.UNIX.Perm, err = r.readUint16()
	return size - 2, err
}

func readUNIXGIDUID(r *Reader, h *Header, size int) (remain int, err error) {
	h.UNIX.GID, _ = r.readUint16()
	h.UNIX.UID, err = r.readUint16()
	return size - 4, err
}

func readUNIXGroup(r *Reader, h *Header, size int) (remain int, err error) {
	h.UNIX.Group, err = r.readStringN(size)
	return 0, err
}

func readUNIXUser(r *Reader, h *Header, size int) (remain int, err error) {
	h.UNIX.User, err = r.readStringN(size)
	return 0, err
}

func readUNIXTime(r *Reader, h *Header, size int) (remain int, err error) {
	h.UNIX.Time, err = r.readTime()
	return size - 4, err
}
