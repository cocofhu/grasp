package workspacesnapshot

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
)

// Decode only the documented v2/v3/v4 entry format and optional extensions.
// Required split/sparse/unknown extensions fail closed. Native Git separately
// validates the private copy; no raw index extensions enter the restored repo.
// Format reference: https://git-scm.com/docs/index-format
func decodeIndex(data []byte, limit int) ([]IndexEntry, string, error) {
	if len(data) == 0 {
		return nil, "", nil
	}
	if len(data) < 12+20 || string(data[:4]) != "DIRC" {
		return nil, "", ErrGitUnsupported
	}
	format, hashSize := "sha1", 20
	valid := false
	if len(data) >= 12+32 {
		hash := sha256.Sum256(data[:len(data)-32])
		if bytes.Equal(hash[:], data[len(data)-32:]) {
			format, hashSize, valid = "sha256", 32, true
		}
	}
	if !valid {
		hash := sha1.Sum(data[:len(data)-20])
		valid = bytes.Equal(hash[:], data[len(data)-20:])
	}
	version := binary.BigEndian.Uint32(data[4:8])
	count := binary.BigEndian.Uint32(data[8:12])
	if !valid || version < 2 || version > 4 || uint64(count) > uint64(limit) {
		return nil, "", ErrGitUnsupported
	}
	end := len(data) - hashSize
	position, previous := 12, ""
	entries := make([]IndexEntry, 0, int(count))
	for range count {
		start := position
		if position > end-40-hashSize-2 {
			return nil, "", ErrGitUnsupported
		}
		mode := binary.BigEndian.Uint32(data[position+24 : position+28])
		oid := hex.EncodeToString(data[position+40 : position+40+hashSize])
		position += 40 + hashSize
		flags := binary.BigEndian.Uint16(data[position : position+2])
		position += 2
		var extended uint16
		if flags&0x4000 != 0 {
			if version == 2 || position > end-2 {
				return nil, "", ErrGitUnsupported
			}
			extended = binary.BigEndian.Uint16(data[position : position+2])
			position += 2
			if extended&^uint16(0x6000) != 0 {
				return nil, "", ErrGitUnsupported
			}
		}
		remove := uint64(0)
		if version == 4 {
			if position >= end {
				return nil, "", ErrGitUnsupported
			}
			c := data[position]
			position++
			remove = uint64(c & 0x7f)
			for iterations := 0; c&0x80 != 0; iterations++ {
				if iterations > 5 || position >= end || remove > uint64(len(previous)) {
					return nil, "", ErrGitUnsupported
				}
				c = data[position]
				position++
				remove = ((remove + 1) << 7) | uint64(c&0x7f)
			}
			if remove > uint64(len(previous)) {
				return nil, "", ErrGitUnsupported
			}
		}
		zero := bytes.IndexByte(data[position:end], 0)
		if zero < 0 || zero > 4096 {
			return nil, "", ErrGitUnsupported
		}
		name := string(data[position : position+zero])
		position += zero + 1
		if version == 4 {
			name = previous[:len(previous)-int(remove)] + name
		} else {
			padded := start + ((position-start+7)/8)*8
			if padded > end || bytes.Count(data[position:padded], []byte{0}) != padded-position {
				return nil, "", ErrGitUnsupported
			}
			position = padded
		}
		if !safePath(name) || name == "." || int(flags&0xfff) != min(len(name), 0xfff) {
			return nil, "", ErrGitUnsupported
		}
		for _, part := range strings.Split(name, "/") {
			if strings.EqualFold(part, ".git") {
				return nil, "", ErrUnsafe
			}
		}
		modeString := strconv.FormatUint(uint64(mode), 8)
		if !slices.Contains([]string{"100644", "100755", "120000", "160000"}, modeString) {
			return nil, "", ErrGitUnsupported
		}
		stage := int((flags >> 12) & 3)
		if len(entries) > 0 {
			last := entries[len(entries)-1]
			if strings.Compare(last.Path, name) > 0 || (last.Path == name && last.Stage >= stage) {
				return nil, "", ErrGitUnsupported
			}
		}
		entries = append(entries, IndexEntry{Path: name, Mode: modeString, OID: oid, Stage: stage, Assume: flags&0x8000 != 0, Skip: extended&0x4000 != 0, Intent: extended&0x2000 != 0})
		previous = name
	}
	for position < end {
		if position > end-8 {
			return nil, "", ErrGitUnsupported
		}
		signature := data[position : position+4]
		size := uint64(binary.BigEndian.Uint32(data[position+4 : position+8]))
		position += 8
		if signature[0] < 'A' || signature[0] > 'Z' || size > uint64(end-position) {
			return nil, "", ErrGitUnsupported
		}
		position += int(size)
	}
	return entries, format, nil
}

func encodeIndex(entries []IndexEntry, format string, limit int64) ([]byte, error) {
	if format != "sha1" && format != "sha256" {
		return nil, ErrGitUnsupported
	}
	entries = slices.Clone(entries)
	slices.SortFunc(entries, func(a, b IndexEntry) int {
		if name := strings.Compare(a.Path, b.Path); name != 0 {
			return name
		}
		return a.Stage - b.Stage
	})
	data := make([]byte, 12)
	copy(data, "DIRC")
	binary.BigEndian.PutUint32(data[4:8], 3)
	binary.BigEndian.PutUint32(data[8:12], uint32(len(entries)))
	for _, entry := range entries {
		start := len(data)
		stat := make([]byte, 40)
		mode, err := strconv.ParseUint(entry.Mode, 8, 32)
		if err != nil {
			return nil, ErrGitUnsupported
		}
		binary.BigEndian.PutUint32(stat[24:28], uint32(mode))
		data = append(data, stat...)
		oid, err := hex.DecodeString(entry.OID)
		if err != nil || (format == "sha1" && len(oid) != 20) || (format == "sha256" && len(oid) != 32) {
			return nil, ErrGitUnsupported
		}
		data = append(data, oid...)
		flags := uint16(min(len(entry.Path), 0xfff)) | (uint16(entry.Stage) << 12)
		if entry.Assume {
			flags |= 0x8000
		}
		var extended uint16
		if entry.Skip {
			extended |= 0x4000
		}
		if entry.Intent {
			extended |= 0x2000
		}
		if extended != 0 {
			flags |= 0x4000
		}
		data = binary.BigEndian.AppendUint16(data, flags)
		if extended != 0 {
			data = binary.BigEndian.AppendUint16(data, extended)
		}
		data = append(data, []byte(entry.Path)...)
		data = append(data, 0)
		for (len(data)-start)%8 != 0 {
			data = append(data, 0)
		}
		if int64(len(data))+32 > limit {
			return nil, ErrLimit
		}
	}
	if format == "sha256" {
		hash := sha256.Sum256(data)
		data = append(data, hash[:]...)
	} else {
		hash := sha1.Sum(data)
		data = append(data, hash[:]...)
	}
	return data, nil
}
