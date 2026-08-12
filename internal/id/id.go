package id

import (
	"crypto/rand"
	"errors"
	"strings"
	"time"
)

const encoding = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

const (
	WorkspacePrefix  = "ws_"
	BasePrefix       = "base_"
	TablePrefix      = "tbl_"
	FieldPrefix      = "fld_"
	ViewPrefix       = "view_"
	RecordPrefix     = "rec_"
	AttachmentPrefix = "att_"
	ChangePrefix     = "chg_"
	ActorPrefix      = "act_"
	TokenPrefix      = "tok_"
	OptionPrefix     = "opt_"
	MutationPrefix   = "mut_"
	RequestPrefix    = "req_"
)

func Valid(prefix, value string) bool {
	if prefix == "" || len(value) != len(prefix)+26 || value[:len(prefix)] != prefix {
		return false
	}
	for _, current := range value[len(prefix):] {
		if current > 127 || !strings.ContainsRune(encoding, current) {
			return false
		}
	}
	return true
}

func New(prefix string) (string, error) {
	if prefix == "" {
		return "", errors.New("id prefix is required")
	}

	var raw [16]byte
	timestamp := uint64(time.Now().UnixMilli())
	raw[0] = byte(timestamp >> 40)
	raw[1] = byte(timestamp >> 32)
	raw[2] = byte(timestamp >> 24)
	raw[3] = byte(timestamp >> 16)
	raw[4] = byte(timestamp >> 8)
	raw[5] = byte(timestamp)
	if _, err := rand.Read(raw[6:]); err != nil {
		return "", err
	}

	return prefix + encode(raw), nil
}

func encode(raw [16]byte) string {
	var encoded [26]byte
	for i := 0; i < len(encoded); i++ {
		var value byte
		for bit := 0; bit < 5; bit++ {
			sourceBit := i*5 + bit - 2
			value <<= 1
			if sourceBit < 0 || sourceBit >= 128 {
				continue
			}
			byteIndex := sourceBit / 8
			bitIndex := 7 - sourceBit%8
			value |= (raw[byteIndex] >> bitIndex) & 1
		}
		encoded[i] = encoding[value]
	}
	return string(encoded[:])
}
