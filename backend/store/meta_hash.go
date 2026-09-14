package store

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"slices"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// metaFingerprintDomain prefixes the hashed bytes so the fingerprint spec is
// versioned: changing the encoding below means bumping the suffix here, which is
// an explicit, reviewable fingerprint change instead of an accidental one.
const metaFingerprintDomain = "metaxisdata.meta.fingerprint.v1\x00"

// CalcStoreMetaHash returns the JSON persisted for a registry resource and the
// fingerprint used to detect changes to it. The two are consistent: hashing the
// returned JSON with CalcMetaHash reproduces the returned fingerprint.
func CalcStoreMetaHash(meta *storepb.StoredMetadata) (metadata []byte, metaHash []byte, err error) {
	metadataBytes, err := protojson.Marshal(meta)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to marshal metadata")
	}

	hash, err := CalcMetaHash(meta)
	if err != nil {
		return nil, nil, err
	}
	return metadataBytes, hash, nil
}

// CalcMetaHash returns the change-detection fingerprint of metadata: the
// SHA-256 of a canonical encoding owned by this package (see
// encodeCanonicalMessage), computed over the statistics-normalized message.
//
// The fingerprint must be a pure function of the semantic content. It therefore
// must not be derived from a protobuf serialization API: protojson output is
// deliberately randomized per binary build (protobuf-go's internal/detrand seeds
// from the executable image), and proto.MarshalOptions{Deterministic: true} is
// documented as non-canonical, unstable over time and unstable across builds
// when unknown fields are present. Either could change every stored hash on a
// rebuild or dependency upgrade, and every restart would then rewrite the whole
// metadata registry.
func CalcMetaHash(meta *storepb.StoredMetadata) ([]byte, error) {
	encoded := encodeCanonicalMessage([]byte(metaFingerprintDomain), normalizeMetadataForHash(meta).ProtoReflect())

	h := sha256.Sum256(encoded)
	return h[:], nil
}

// normalizeMetadataForHash returns a clone of the given metadata with volatile
// statistics fields zeroed out, so the fingerprint is stable across syncs when
// only statistics (row counts, data sizes, etc.) change rather than the schema.
func normalizeMetadataForHash(meta *storepb.StoredMetadata) *storepb.StoredMetadata {
	cloned, ok := proto.Clone(meta).(*storepb.StoredMetadata)
	if !ok {
		return meta
	}

	switch {
	case cloned.GetTableMetadata() != nil:
		tm := cloned.GetTableMetadata()
		tm.RowCount = 0
		tm.DataSize = 0
		tm.IndexSize = 0
		tm.DataFree = 0
	case cloned.GetSequenceMetadata() != nil:
		sm := cloned.GetSequenceMetadata()
		sm.LastValue = ""
	default:
	}

	return cloned
}

// encodeCanonicalMessage appends a canonical encoding of msg to dst. The format
// is private to this package and defined only by the properties the fingerprint
// needs:
//
//   - Only populated fields are encoded, visited in ascending field-number order,
//     so the encoding does not depend on map iteration order, on the order fields
//     happen to be set, or on the declaration order of the .proto. Unknown fields
//     are ignored, so a message written by a different schema version fingerprints
//     the same.
//   - Lengths and field numbers are unsigned varints; scalar values are fixed
//     width or length prefixed; enums are encoded by number, so renaming an enum
//     value does not change the fingerprint.
//   - Repeated fields preserve order. Map entries are encoded independently and
//     sorted by their bytes, so map order never matters.
//   - Sub-messages are length prefixed and encoded recursively. Only populated
//     fields contribute, so a new .proto field that is left unset does not move
//     the fingerprint of existing rows.
func encodeCanonicalMessage(dst []byte, msg protoreflect.Message) []byte {
	fields := make([]protoreflect.FieldDescriptor, 0, msg.Descriptor().Fields().Len())
	msg.Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		fields = append(fields, fd)
		return true
	})
	slices.SortFunc(fields, func(a, b protoreflect.FieldDescriptor) int {
		return cmp.Compare(a.Number(), b.Number())
	})

	dst = appendCanonicalUvarint(dst, uint64(len(fields)))
	for _, fd := range fields {
		dst = appendCanonicalUvarint(dst, uint64(fd.Number()))
		dst = encodeCanonicalField(dst, fd, msg.Get(fd))
	}
	return dst
}

func encodeCanonicalField(dst []byte, fd protoreflect.FieldDescriptor, v protoreflect.Value) []byte {
	switch {
	case fd.IsMap():
		return encodeCanonicalMap(dst, fd, v.Map())
	case fd.IsList():
		list := v.List()
		dst = appendCanonicalUvarint(dst, uint64(list.Len()))
		for i := range list.Len() {
			dst = encodeCanonicalValue(dst, fd, list.Get(i))
		}
		return dst
	default:
		return encodeCanonicalValue(dst, fd, v)
	}
}

func encodeCanonicalMap(dst []byte, fd protoreflect.FieldDescriptor, m protoreflect.Map) []byte {
	entries := make([][]byte, 0, m.Len())
	m.Range(func(k protoreflect.MapKey, v protoreflect.Value) bool {
		entry := encodeCanonicalValue(nil, fd.MapKey(), k.Value())
		entry = encodeCanonicalValue(entry, fd.MapValue(), v)
		entries = append(entries, entry)
		return true
	})
	slices.SortFunc(entries, bytes.Compare)

	dst = appendCanonicalUvarint(dst, uint64(len(entries)))
	for _, entry := range entries {
		dst = appendCanonicalLengthPrefixed(dst, entry)
	}
	return dst
}

func encodeCanonicalValue(dst []byte, fd protoreflect.FieldDescriptor, v protoreflect.Value) []byte {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		if v.Bool() {
			return append(dst, 1)
		}
		return append(dst, 0)
	case protoreflect.EnumKind:
		return appendCanonicalUvarint(dst, uint64(int64(v.Enum())))
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return appendCanonicalUvarint(dst, uint64(int64(int32(v.Int()))))
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return appendCanonicalUvarint(dst, uint64(v.Int()))
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return appendCanonicalUvarint(dst, uint64(uint32(v.Uint())))
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return appendCanonicalUvarint(dst, v.Uint())
	case protoreflect.FloatKind:
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(float32(v.Float())))
		return append(dst, b[:]...)
	case protoreflect.DoubleKind:
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], math.Float64bits(v.Float()))
		return append(dst, b[:]...)
	case protoreflect.StringKind:
		return appendCanonicalString(dst, v.String())
	case protoreflect.BytesKind:
		return appendCanonicalLengthPrefixed(dst, v.Bytes())
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return appendCanonicalLengthPrefixed(dst, encodeCanonicalMessage(nil, v.Message()))
	default:
		// Every valid protoreflect.Kind is handled above; the zero value never
		// reaches here because field descriptors always carry a valid kind.
		return dst
	}
}

func appendCanonicalString(dst []byte, s string) []byte {
	dst = appendCanonicalUvarint(dst, uint64(len(s)))
	return append(dst, s...)
}

func appendCanonicalLengthPrefixed(dst, b []byte) []byte {
	dst = appendCanonicalUvarint(dst, uint64(len(b)))
	return append(dst, b...)
}

func appendCanonicalUvarint(dst []byte, v uint64) []byte {
	var tmp [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tmp[:], v)
	return append(dst, tmp[:n]...)
}
