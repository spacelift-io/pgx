package pgtype

import (
	"bytes"
	"fmt"
	"io"

	"github.com/jackc/pgx/pgio"
)

type ACLItemArray struct {
	Elements   []ACLItem
	Dimensions []ArrayDimension
	Status     Status
}

func (dst *ACLItemArray) ConvertFrom(src interface{}) error {
	switch value := src.(type) {
	case ACLItemArray:
		*dst = value

	case []string:
		if value == nil {
			*dst = ACLItemArray{Status: Null}
		} else if len(value) == 0 {
			*dst = ACLItemArray{Status: Present}
		} else {
			elements := make([]ACLItem, len(value))
			for i := range value {
				if err := elements[i].ConvertFrom(value[i]); err != nil {
					return err
				}
			}
			*dst = ACLItemArray{
				Elements:   elements,
				Dimensions: []ArrayDimension{{Length: int32(len(elements)), LowerBound: 1}},
				Status:     Present,
			}
		}

	default:
		if originalSrc, ok := underlyingSliceType(src); ok {
			return dst.ConvertFrom(originalSrc)
		}
		return fmt.Errorf("cannot convert %v to ACLItem", value)
	}

	return nil
}

func (src *ACLItemArray) AssignTo(dst interface{}) error {
	switch v := dst.(type) {

	case *[]string:
		if src.Status == Present {
			*v = make([]string, len(src.Elements))
			for i := range src.Elements {
				if err := src.Elements[i].AssignTo(&((*v)[i])); err != nil {
					return err
				}
			}
		} else {
			*v = nil
		}

	default:
		if originalDst, ok := underlyingPtrSliceType(dst); ok {
			return src.AssignTo(originalDst)
		}
		return fmt.Errorf("cannot decode %v into %T", src, dst)
	}

	return nil
}

func (dst *ACLItemArray) DecodeText(src []byte) error {
	if src == nil {
		*dst = ACLItemArray{Status: Null}
		return nil
	}

	uta, err := ParseUntypedTextArray(string(src))
	if err != nil {
		return err
	}

	var elements []ACLItem

	if len(uta.Elements) > 0 {
		elements = make([]ACLItem, len(uta.Elements))

		for i, s := range uta.Elements {
			var elem ACLItem
			var elemSrc []byte
			if s != "NULL" {
				elemSrc = []byte(s)
			}
			err = elem.DecodeText(elemSrc)
			if err != nil {
				return err
			}

			elements[i] = elem
		}
	}

	*dst = ACLItemArray{Elements: elements, Dimensions: uta.Dimensions, Status: Present}

	return nil
}

func (src *ACLItemArray) EncodeText(w io.Writer) (bool, error) {
	switch src.Status {
	case Null:
		return true, nil
	case Undefined:
		return false, errUndefined
	}

	if len(src.Dimensions) == 0 {
		_, err := io.WriteString(w, "{}")
		return false, err
	}

	err := EncodeTextArrayDimensions(w, src.Dimensions)
	if err != nil {
		return false, err
	}

	// dimElemCounts is the multiples of elements that each array lies on. For
	// example, a single dimension array of length 4 would have a dimElemCounts of
	// [4]. A multi-dimensional array of lengths [3,5,2] would have a
	// dimElemCounts of [30,10,2]. This is used to simplify when to render a '{'
	// or '}'.
	dimElemCounts := make([]int, len(src.Dimensions))
	dimElemCounts[len(src.Dimensions)-1] = int(src.Dimensions[len(src.Dimensions)-1].Length)
	for i := len(src.Dimensions) - 2; i > -1; i-- {
		dimElemCounts[i] = int(src.Dimensions[i].Length) * dimElemCounts[i+1]
	}

	for i, elem := range src.Elements {
		if i > 0 {
			err = pgio.WriteByte(w, ',')
			if err != nil {
				return false, err
			}
		}

		for _, dec := range dimElemCounts {
			if i%dec == 0 {
				err = pgio.WriteByte(w, '{')
				if err != nil {
					return false, err
				}
			}
		}

		elemBuf := &bytes.Buffer{}
		null, err := elem.EncodeText(elemBuf)
		if err != nil {
			return false, err
		}
		if null {
			_, err = io.WriteString(w, `NULL`)
			if err != nil {
				return false, err
			}
		} else {
			_, err = io.WriteString(w, QuoteArrayElementIfNeeded(elemBuf.String()))
			if err != nil {
				return false, err
			}
		}

		for _, dec := range dimElemCounts {
			if (i+1)%dec == 0 {
				err = pgio.WriteByte(w, '}')
				if err != nil {
					return false, err
				}
			}
		}
	}

	return false, nil
}