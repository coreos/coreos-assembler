package encoder

import (
	"bytes"

	"github.com/goccy/go-json/internal/errors"
)

func Compact(dst *bytes.Buffer, src []byte, escape bool) error {
	if len(src) == 0 {
		return errors.ErrUnexpectedEndOfJSON("", 0)
	}
	length := len(src)
	for cursor := 0; cursor < length; cursor++ {
		c := src[cursor]
		switch c {
		case ' ', '\t', '\n', '\r':
			continue
		case '"':
			if err := dst.WriteByte(c); err != nil {
				return err
			}
			for {
				cursor++
				c := src[cursor]
				if escape && (c == '<' || c == '>' || c == '&') {
					if _, err := dst.WriteString(`\u00`); err != nil {
						return err
					}
					if _, err := dst.Write([]byte{hex[c>>4], hex[c&0xF]}); err != nil {
						return err
					}
				} else if err := dst.WriteByte(c); err != nil {
					return err
				}
				switch c {
				case '\\':
					cursor++
					if err := dst.WriteByte(src[cursor]); err != nil {
						return err
					}
				case '"':
					goto LOOP_END
				case '\000':
					return errors.ErrUnexpectedEndOfJSON("string", int64(length))
				}
			}
		default:
			if err := dst.WriteByte(c); err != nil {
				return err
			}
		}
	LOOP_END:
	}
	return nil
}
