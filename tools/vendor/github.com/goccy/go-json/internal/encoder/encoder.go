package encoder

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/runtime"
)

type Option int

const (
	HTMLEscapeOption Option = 1 << iota
	IndentOption
	UnorderedMapOption
)

func (t OpType) IsMultipleOpHead() bool {
	switch t {
	case OpStructHead:
		return true
	case OpStructHeadSlice:
		return true
	case OpStructHeadArray:
		return true
	case OpStructHeadMap:
		return true
	case OpStructHeadStruct:
		return true
	case OpStructHeadOmitEmpty:
		return true
	case OpStructHeadOmitEmptySlice:
		return true
	case OpStructHeadStringTagSlice:
		return true
	case OpStructHeadOmitEmptyArray:
		return true
	case OpStructHeadStringTagArray:
		return true
	case OpStructHeadOmitEmptyMap:
		return true
	case OpStructHeadStringTagMap:
		return true
	case OpStructHeadOmitEmptyStruct:
		return true
	case OpStructHeadStringTag:
		return true
	case OpStructHeadSlicePtr:
		return true
	case OpStructHeadOmitEmptySlicePtr:
		return true
	case OpStructHeadStringTagSlicePtr:
		return true
	case OpStructHeadArrayPtr:
		return true
	case OpStructHeadOmitEmptyArrayPtr:
		return true
	case OpStructHeadStringTagArrayPtr:
		return true
	case OpStructHeadMapPtr:
		return true
	case OpStructHeadOmitEmptyMapPtr:
		return true
	case OpStructHeadStringTagMapPtr:
		return true
	}
	return false
}

func (t OpType) IsMultipleOpField() bool {
	switch t {
	case OpStructField:
		return true
	case OpStructFieldSlice:
		return true
	case OpStructFieldArray:
		return true
	case OpStructFieldMap:
		return true
	case OpStructFieldStruct:
		return true
	case OpStructFieldOmitEmpty:
		return true
	case OpStructFieldOmitEmptySlice:
		return true
	case OpStructFieldStringTagSlice:
		return true
	case OpStructFieldOmitEmptyArray:
		return true
	case OpStructFieldStringTagArray:
		return true
	case OpStructFieldOmitEmptyMap:
		return true
	case OpStructFieldStringTagMap:
		return true
	case OpStructFieldOmitEmptyStruct:
		return true
	case OpStructFieldStringTag:
		return true
	case OpStructFieldSlicePtr:
		return true
	case OpStructFieldOmitEmptySlicePtr:
		return true
	case OpStructFieldStringTagSlicePtr:
		return true
	case OpStructFieldArrayPtr:
		return true
	case OpStructFieldOmitEmptyArrayPtr:
		return true
	case OpStructFieldStringTagArrayPtr:
		return true
	case OpStructFieldMapPtr:
		return true
	case OpStructFieldOmitEmptyMapPtr:
		return true
	case OpStructFieldStringTagMapPtr:
		return true
	}
	return false
}

type OpcodeSet struct {
	Code       *Opcode
	CodeLength int
}

type CompiledCode struct {
	Code    *Opcode
	Linked  bool // whether recursive code already have linked
	CurLen  uintptr
	NextLen uintptr
}

const StartDetectingCyclesAfter = 1000

func Load(base uintptr, idx uintptr) uintptr {
	addr := base + idx
	return **(**uintptr)(unsafe.Pointer(&addr))
}

func Store(base uintptr, idx uintptr, p uintptr) {
	addr := base + idx
	**(**uintptr)(unsafe.Pointer(&addr)) = p
}

func LoadNPtr(base uintptr, idx uintptr, ptrNum int) uintptr {
	addr := base + idx
	p := **(**uintptr)(unsafe.Pointer(&addr))
	if p == 0 {
		return 0
	}
	return PtrToPtr(p)
	/*
		for i := 0; i < ptrNum; i++ {
			if p == 0 {
				return p
			}
			p = PtrToPtr(p)
		}
		return p
	*/
}

func PtrToUint64(p uintptr) uint64              { return **(**uint64)(unsafe.Pointer(&p)) }
func PtrToFloat32(p uintptr) float32            { return **(**float32)(unsafe.Pointer(&p)) }
func PtrToFloat64(p uintptr) float64            { return **(**float64)(unsafe.Pointer(&p)) }
func PtrToBool(p uintptr) bool                  { return **(**bool)(unsafe.Pointer(&p)) }
func PtrToBytes(p uintptr) []byte               { return **(**[]byte)(unsafe.Pointer(&p)) }
func PtrToNumber(p uintptr) json.Number         { return **(**json.Number)(unsafe.Pointer(&p)) }
func PtrToString(p uintptr) string              { return **(**string)(unsafe.Pointer(&p)) }
func PtrToSlice(p uintptr) *runtime.SliceHeader { return *(**runtime.SliceHeader)(unsafe.Pointer(&p)) }
func PtrToPtr(p uintptr) uintptr {
	return uintptr(**(**unsafe.Pointer)(unsafe.Pointer(&p)))
}
func PtrToNPtr(p uintptr, ptrNum int) uintptr {
	for i := 0; i < ptrNum; i++ {
		if p == 0 {
			return 0
		}
		p = PtrToPtr(p)
	}
	return p
}

func PtrToUnsafePtr(p uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}
func PtrToInterface(code *Opcode, p uintptr) interface{} {
	return *(*interface{})(unsafe.Pointer(&emptyInterface{
		typ: code.Type,
		ptr: *(*unsafe.Pointer)(unsafe.Pointer(&p)),
	}))
}

func ErrUnsupportedValue(code *Opcode, ptr uintptr) *errors.UnsupportedValueError {
	v := *(*interface{})(unsafe.Pointer(&emptyInterface{
		typ: code.Type,
		ptr: *(*unsafe.Pointer)(unsafe.Pointer(&ptr)),
	}))
	return &errors.UnsupportedValueError{
		Value: reflect.ValueOf(v),
		Str:   fmt.Sprintf("encountered a cycle via %s", code.Type),
	}
}

func ErrUnsupportedFloat(v float64) *errors.UnsupportedValueError {
	return &errors.UnsupportedValueError{
		Value: reflect.ValueOf(v),
		Str:   strconv.FormatFloat(v, 'g', -1, 64),
	}
}

func ErrMarshalerWithCode(code *Opcode, err error) *errors.MarshalerError {
	return &errors.MarshalerError{
		Type: runtime.RType2Type(code.Type),
		Err:  err,
	}
}

type emptyInterface struct {
	typ *runtime.Type
	ptr unsafe.Pointer
}

type MapItem struct {
	Key   []byte
	Value []byte
}

type Mapslice struct {
	Items []MapItem
}

func (m *Mapslice) Len() int {
	return len(m.Items)
}

func (m *Mapslice) Less(i, j int) bool {
	return bytes.Compare(m.Items[i].Key, m.Items[j].Key) < 0
}

func (m *Mapslice) Swap(i, j int) {
	m.Items[i], m.Items[j] = m.Items[j], m.Items[i]
}

type MapContext struct {
	Pos   []int
	Slice *Mapslice
	Buf   []byte
}

var mapContextPool = sync.Pool{
	New: func() interface{} {
		return &MapContext{}
	},
}

func NewMapContext(mapLen int) *MapContext {
	ctx := mapContextPool.Get().(*MapContext)
	if ctx.Slice == nil {
		ctx.Slice = &Mapslice{
			Items: make([]MapItem, 0, mapLen),
		}
	}
	if cap(ctx.Pos) < (mapLen*2 + 1) {
		ctx.Pos = make([]int, 0, mapLen*2+1)
		ctx.Slice.Items = make([]MapItem, 0, mapLen)
	} else {
		ctx.Pos = ctx.Pos[:0]
		ctx.Slice.Items = ctx.Slice.Items[:0]
	}
	ctx.Buf = ctx.Buf[:0]
	return ctx
}

func ReleaseMapContext(c *MapContext) {
	mapContextPool.Put(c)
}

//go:linkname MapIterInit reflect.mapiterinit
//go:noescape
func MapIterInit(mapType *runtime.Type, m unsafe.Pointer) unsafe.Pointer

//go:linkname MapIterKey reflect.mapiterkey
//go:noescape
func MapIterKey(it unsafe.Pointer) unsafe.Pointer

//go:linkname MapIterNext reflect.mapiternext
//go:noescape
func MapIterNext(it unsafe.Pointer)

//go:linkname MapLen reflect.maplen
//go:noescape
func MapLen(m unsafe.Pointer) int

type RuntimeContext struct {
	Buf        []byte
	Ptrs       []uintptr
	KeepRefs   []unsafe.Pointer
	SeenPtr    []uintptr
	BaseIndent int
	Prefix     []byte
	IndentStr  []byte
}

func (c *RuntimeContext) Init(p uintptr, codelen int) {
	if len(c.Ptrs) < codelen {
		c.Ptrs = make([]uintptr, codelen)
	}
	c.Ptrs[0] = p
	c.KeepRefs = c.KeepRefs[:0]
	c.SeenPtr = c.SeenPtr[:0]
	c.BaseIndent = 0
}

func (c *RuntimeContext) Ptr() uintptr {
	header := (*runtime.SliceHeader)(unsafe.Pointer(&c.Ptrs))
	return uintptr(header.Data)
}

func AppendByteSlice(b []byte, src []byte) []byte {
	if src == nil {
		return append(b, `null`...)
	}
	encodedLen := base64.StdEncoding.EncodedLen(len(src))
	b = append(b, '"')
	pos := len(b)
	remainLen := cap(b[pos:])
	var buf []byte
	if remainLen > encodedLen {
		buf = b[pos : pos+encodedLen]
	} else {
		buf = make([]byte, encodedLen)
	}
	base64.StdEncoding.Encode(buf, src)
	return append(append(b, buf...), '"')
}

func AppendFloat32(b []byte, v float32) []byte {
	f64 := float64(v)
	abs := math.Abs(f64)
	fmt := byte('f')
	// Note: Must use float32 comparisons for underlying float32 value to get precise cutoffs right.
	if abs != 0 {
		f32 := float32(abs)
		if f32 < 1e-6 || f32 >= 1e21 {
			fmt = 'e'
		}
	}
	return strconv.AppendFloat(b, f64, fmt, -1, 32)
}

func AppendFloat64(b []byte, v float64) []byte {
	abs := math.Abs(v)
	fmt := byte('f')
	// Note: Must use float32 comparisons for underlying float32 value to get precise cutoffs right.
	if abs != 0 {
		if abs < 1e-6 || abs >= 1e21 {
			fmt = 'e'
		}
	}
	return strconv.AppendFloat(b, v, fmt, -1, 64)
}

func AppendBool(b []byte, v bool) []byte {
	if v {
		return append(b, "true"...)
	}
	return append(b, "false"...)
}

var (
	floatTable = [256]bool{
		'0': true,
		'1': true,
		'2': true,
		'3': true,
		'4': true,
		'5': true,
		'6': true,
		'7': true,
		'8': true,
		'9': true,
		'.': true,
		'e': true,
		'E': true,
		'+': true,
		'-': true,
	}
)

func AppendNumber(b []byte, n json.Number) ([]byte, error) {
	if len(n) == 0 {
		return append(b, '0'), nil
	}
	for i := 0; i < len(n); i++ {
		if !floatTable[n[i]] {
			return nil, fmt.Errorf("json: invalid number literal %q", n)
		}
	}
	b = append(b, n...)
	return b, nil
}

func AppendMarshalJSON(code *Opcode, b []byte, v interface{}, escape bool) ([]byte, error) {
	rv := reflect.ValueOf(v) // convert by dynamic interface type
	if code.AddrForMarshaler {
		if rv.CanAddr() {
			rv = rv.Addr()
		} else {
			newV := reflect.New(rv.Type())
			newV.Elem().Set(rv)
			rv = newV
		}
	}
	v = rv.Interface()
	marshaler, ok := v.(json.Marshaler)
	if !ok {
		return AppendNull(b), nil
	}
	bb, err := marshaler.MarshalJSON()
	if err != nil {
		return nil, &errors.MarshalerError{Type: reflect.TypeOf(v), Err: err}
	}
	buf := bytes.NewBuffer(b)
	// TODO: we should validate buffer with `compact`
	if err := Compact(buf, bb, escape); err != nil {
		return nil, &errors.MarshalerError{Type: reflect.TypeOf(v), Err: err}
	}
	return buf.Bytes(), nil
}

func AppendMarshalJSONIndent(ctx *RuntimeContext, code *Opcode, b []byte, v interface{}, indent int, escape bool) ([]byte, error) {
	rv := reflect.ValueOf(v) // convert by dynamic interface type
	if code.AddrForMarshaler {
		if rv.CanAddr() {
			rv = rv.Addr()
		} else {
			newV := reflect.New(rv.Type())
			newV.Elem().Set(rv)
			rv = newV
		}
	}
	v = rv.Interface()
	marshaler, ok := v.(json.Marshaler)
	if !ok {
		return AppendNull(b), nil
	}
	bb, err := marshaler.MarshalJSON()
	if err != nil {
		return nil, &errors.MarshalerError{Type: reflect.TypeOf(v), Err: err}
	}
	var compactBuf bytes.Buffer
	if err := Compact(&compactBuf, bb, escape); err != nil {
		return nil, &errors.MarshalerError{Type: reflect.TypeOf(v), Err: err}
	}
	var indentBuf bytes.Buffer
	if err := Indent(
		&indentBuf,
		compactBuf.Bytes(),
		string(ctx.Prefix)+strings.Repeat(string(ctx.IndentStr), ctx.BaseIndent+indent),
		string(ctx.IndentStr),
	); err != nil {
		return nil, &errors.MarshalerError{Type: reflect.TypeOf(v), Err: err}
	}
	return append(b, indentBuf.Bytes()...), nil
}

func AppendMarshalText(code *Opcode, b []byte, v interface{}, escape bool) ([]byte, error) {
	rv := reflect.ValueOf(v) // convert by dynamic interface type
	if code.AddrForMarshaler {
		if rv.CanAddr() {
			rv = rv.Addr()
		} else {
			newV := reflect.New(rv.Type())
			newV.Elem().Set(rv)
			rv = newV
		}
	}
	v = rv.Interface()
	marshaler, ok := v.(encoding.TextMarshaler)
	if !ok {
		return AppendNull(b), nil
	}
	bytes, err := marshaler.MarshalText()
	if err != nil {
		return nil, &errors.MarshalerError{Type: reflect.TypeOf(v), Err: err}
	}
	if escape {
		return AppendEscapedString(b, *(*string)(unsafe.Pointer(&bytes))), nil
	}
	return AppendString(b, *(*string)(unsafe.Pointer(&bytes))), nil
}

func AppendMarshalTextIndent(code *Opcode, b []byte, v interface{}, escape bool) ([]byte, error) {
	rv := reflect.ValueOf(v) // convert by dynamic interface type
	if code.AddrForMarshaler {
		if rv.CanAddr() {
			rv = rv.Addr()
		} else {
			newV := reflect.New(rv.Type())
			newV.Elem().Set(rv)
			rv = newV
		}
	}
	v = rv.Interface()
	marshaler, ok := v.(encoding.TextMarshaler)
	if !ok {
		return AppendNull(b), nil
	}
	bytes, err := marshaler.MarshalText()
	if err != nil {
		return nil, &errors.MarshalerError{Type: reflect.TypeOf(v), Err: err}
	}
	if escape {
		return AppendEscapedString(b, *(*string)(unsafe.Pointer(&bytes))), nil
	}
	return AppendString(b, *(*string)(unsafe.Pointer(&bytes))), nil
}

func AppendNull(b []byte) []byte {
	return append(b, "null"...)
}

func AppendComma(b []byte) []byte {
	return append(b, ',')
}

func AppendCommaIndent(b []byte) []byte {
	return append(b, ',', '\n')
}

func AppendStructEnd(b []byte) []byte {
	return append(b, '}', ',')
}

func AppendStructEndIndent(ctx *RuntimeContext, b []byte, indent int) []byte {
	b = append(b, '\n')
	b = append(b, ctx.Prefix...)
	b = append(b, bytes.Repeat(ctx.IndentStr, ctx.BaseIndent+indent)...)
	return append(b, '}', ',', '\n')
}

func AppendIndent(ctx *RuntimeContext, b []byte, indent int) []byte {
	b = append(b, ctx.Prefix...)
	return append(b, bytes.Repeat(ctx.IndentStr, ctx.BaseIndent+indent)...)
}

func IsNilForMarshaler(v interface{}) bool {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Interface, reflect.Map, reflect.Ptr:
		return rv.IsNil()
	case reflect.Slice:
		return rv.IsNil() || rv.Len() == 0
	}
	return false
}
