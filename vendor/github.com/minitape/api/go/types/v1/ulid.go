package typesv1

import "encoding/binary"

func ULIDFromBytes(out *ULID, ulid [16]byte) *ULID {
	if out == nil {
		out = new(ULID)
	}
	out.High = binary.BigEndian.Uint64(ulid[0:8])
	out.Low = binary.BigEndian.Uint64(ulid[8:16])
	return out
}

func ULIDToBytes(b []byte, in *ULID) [16]byte {
	b = binary.BigEndian.AppendUint64(b, in.High)
	b = binary.BigEndian.AppendUint64(b, in.Low)
	return *(*[16]byte)(b[:16])
}

func (ul *ULID) Compare(other *ULID) int {
	if ul.High < other.High {
		return -1
	} else if ul.High > other.High {
		return 1
	}
	if ul.Low < other.Low {
		return -1
	} else if ul.Low > other.Low {
		return 1
	}
	return 0
}
