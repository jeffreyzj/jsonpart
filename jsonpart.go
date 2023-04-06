package jsonpart

// fork from fastjson

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf16"
	"unsafe"
)

// Parse s contain json string embedded in, get partial value by specified key
// full s json will be parse if partialKey is empty or ""
func Parse(s string, partialKey ...string) (*Value, error) {
	// find partialKey
	if len(partialKey) > 0 && len(partialKey[0]) > 0 {
		i := strings.Index(s, fmt.Sprintf("\"%s\"", partialKey[0]))
		if i == -1 {
			return nil, fmt.Errorf("cannot find partialKey: \"%s\"; JSON: %q", partialKey[0], startEndString(s))
		}
		s = s[i:]
		v := s[len(partialKey[0])+2:]
		v = skipWS(v)
		if v[0] != ':' {
			return nil, fmt.Errorf("invalid partialKey: \"%s\"; JSON: %q", partialKey[0], startEndString(s))
		}
		s = v[1:]
	}
	p := &parser{}
	return p.parse(s)
}

func ParseBytes(b []byte, partialKey ...string) (*Value, error) {
	return Parse(b2s(b), partialKey...)
}

// parser parses JSON.
//
// parser may be re-used for subsequent parsing.
//
// parser cannot be used from concurrent goroutines.
// Use per-goroutine parsers or ParserPool instead.
type parser struct {
	// b contains working copy of the string to be parsed.
	b []byte

	// c is a cache for json values.
	c cache
}

func (p *parser) parse(s string) (*Value, error) {
	s = skipWS(s)
	p.b = append(p.b[:0], s...)
	p.c.reset()

	v, tail, err := parseValue(b2s(p.b), &p.c, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot parse JSON: %s; unparsed tail: %q", err, startEndString(tail))
	}
	return v, nil
}

type cache struct {
	vs []Value
}

func (c *cache) reset() {
	c.vs = c.vs[:0]
}

func (c *cache) getValue() *Value {
	if cap(c.vs) > len(c.vs) {
		c.vs = c.vs[:len(c.vs)+1]
	} else {
		c.vs = append(c.vs, Value{})
	}
	// Do not reset the value, since the caller must properly init it.
	return &c.vs[len(c.vs)-1]
}

// vType represents JSON type.
type vType int

const (
	// typeNull is JSON null.
	typeNull vType = 0

	// typeObject is JSON object type.
	typeObject vType = 1

	// typeArray is JSON array type.
	typeArray vType = 2

	// typeString is JSON string type.
	typeString vType = 3

	// typeNumber is JSON number type.
	typeNumber vType = 4

	// typeTrue is JSON true.
	typeTrue vType = 5

	// typeFalse is JSON false.
	typeFalse vType = 6

	typeRawString vType = 7
)

// String returns string representation of t.
func (t vType) String() string {
	switch t {
	case typeObject:
		return "object"
	case typeArray:
		return "array"
	case typeString:
		return "string"
	case typeNumber:
		return "number"
	case typeTrue:
		return "true"
	case typeFalse:
		return "false"
	case typeNull:
		return "null"

	// typeRawString is skipped intentionally,
	// since it shouldn't be visible to user.
	default:
		panic(fmt.Errorf("BUG: unknown Value type: %d", t))
	}
}

// maxDepth is the maximum depth for nested JSON.
const maxDepth = 300

func parseValue(s string, c *cache, depth int) (*Value, string, error) {
	if len(s) == 0 {
		return nil, s, fmt.Errorf("cannot parse empty string")
	}
	depth++
	if depth > maxDepth {
		return nil, s, fmt.Errorf("too big depth for the nested JSON; it exceeds %d", maxDepth)
	}

	if s[0] == '{' {
		v, tail, err := parseObject(s[1:], c, depth)
		if err != nil {
			return nil, tail, fmt.Errorf("cannot parse object: %s", err)
		}
		return v, tail, nil
	}
	if s[0] == '[' {
		v, tail, err := parseArray(s[1:], c, depth)
		if err != nil {
			return nil, tail, fmt.Errorf("cannot parse array: %s", err)
		}
		return v, tail, nil
	}
	if s[0] == '"' {
		ss, tail, err := parseRawString(s[1:])
		if err != nil {
			return nil, tail, fmt.Errorf("cannot parse string: %s", err)
		}
		v := c.getValue()
		v.t = typeRawString
		v.s = ss
		return v, tail, nil
	}
	if s[0] == 't' {
		if len(s) < len("true") || s[:len("true")] != "true" {
			return nil, s, fmt.Errorf("unexpected value found: %q", s)
		}
		return valueTrue, s[len("true"):], nil
	}
	if s[0] == 'f' {
		if len(s) < len("false") || s[:len("false")] != "false" {
			return nil, s, fmt.Errorf("unexpected value found: %q", s)
		}
		return valueFalse, s[len("false"):], nil
	}
	if s[0] == 'n' {
		if len(s) < len("null") || s[:len("null")] != "null" {
			// Try parsing NaN
			if len(s) >= 3 && strings.EqualFold(s[:3], "nan") {
				v := c.getValue()
				v.t = typeNumber
				v.s = s[:3]
				return v, s[3:], nil
			}
			return nil, s, fmt.Errorf("unexpected value found: %q", s)
		}
		return valueNull, s[len("null"):], nil
	}

	ns, tail, err := parseRawNumber(s)
	if err != nil {
		return nil, tail, fmt.Errorf("cannot parse number: %s", err)
	}
	v := c.getValue()
	v.t = typeNumber
	v.s = ns
	return v, tail, nil
}

func parseArray(s string, c *cache, depth int) (*Value, string, error) {
	s = skipWS(s)
	if len(s) == 0 {
		return nil, s, fmt.Errorf("missing ']'")
	}

	if s[0] == ']' {
		v := c.getValue()
		v.t = typeArray
		v.a = v.a[:0]
		return v, s[1:], nil
	}

	a := c.getValue()
	a.t = typeArray
	a.a = a.a[:0]
	for {
		var v *Value
		var err error

		s = skipWS(s)
		v, s, err = parseValue(s, c, depth)
		if err != nil {
			return nil, s, fmt.Errorf("cannot parse array value: %s", err)
		}
		a.a = append(a.a, v)

		s = skipWS(s)
		if len(s) == 0 {
			return nil, s, fmt.Errorf("unexpected end of array")
		}
		if s[0] == ',' {
			s = s[1:]
			continue
		}
		if s[0] == ']' {
			s = s[1:]
			return a, s, nil
		}
		return nil, s, fmt.Errorf("missing ',' after array value")
	}
}

func parseObject(s string, c *cache, depth int) (*Value, string, error) {
	s = skipWS(s)
	if len(s) == 0 {
		return nil, s, fmt.Errorf("missing '}'")
	}

	if s[0] == '}' {
		v := c.getValue()
		v.t = typeObject
		v.o.reset()
		return v, s[1:], nil
	}

	o := c.getValue()
	o.t = typeObject
	o.o.reset()
	for {
		var err error
		kv := o.o.getKV()

		// parse key.
		s = skipWS(s)
		if len(s) == 0 || s[0] != '"' {
			return nil, s, fmt.Errorf(`cannot find opening '"" for object key`)
		}
		kv.k, s, err = parseRawKey(s[1:])
		if err != nil {
			return nil, s, fmt.Errorf("cannot parse object key: %s", err)
		}
		s = skipWS(s)
		if len(s) == 0 || s[0] != ':' {
			return nil, s, fmt.Errorf("missing ':' after object key")
		}
		s = s[1:]

		// parse value
		s = skipWS(s)
		kv.v, s, err = parseValue(s, c, depth)
		if err != nil {
			return nil, s, fmt.Errorf("cannot parse object value: %s", err)
		}
		s = skipWS(s)
		if len(s) == 0 {
			return nil, s, fmt.Errorf("unexpected end of object")
		}
		if s[0] == ',' {
			s = s[1:]
			continue
		}
		if s[0] == '}' {
			return o, s[1:], nil
		}
		return nil, s, fmt.Errorf("missing ',' after object value")
	}
}

func escapeString(dst []byte, s string) []byte {
	if !hasSpecialChars(s) {
		// Fast path - nothing to escape.
		dst = append(dst, '"')
		dst = append(dst, s...)
		dst = append(dst, '"')
		return dst
	}

	// Slow path.
	return strconv.AppendQuote(dst, s)
}

func hasSpecialChars(s string) bool {
	if strings.IndexByte(s, '"') >= 0 || strings.IndexByte(s, '\\') >= 0 {
		return true
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 {
			return true
		}
	}
	return false
}

func unescapeStringBestEffort(s string) string {
	n := strings.IndexByte(s, '\\')
	if n < 0 {
		// Fast path - nothing to unescape.
		return s
	}

	// Slow path - unescape string.
	b := s2b(s) // It is safe to do, since s points to a byte slice in parser.b.
	b = b[:n]
	s = s[n+1:]
	for len(s) > 0 {
		ch := s[0]
		s = s[1:]
		switch ch {
		case '"':
			b = append(b, '"')
		case '\\':
			b = append(b, '\\')
		case '/':
			b = append(b, '/')
		case 'b':
			b = append(b, '\b')
		case 'f':
			b = append(b, '\f')
		case 'n':
			b = append(b, '\n')
		case 'r':
			b = append(b, '\r')
		case 't':
			b = append(b, '\t')
		case 'u':
			if len(s) < 4 {
				// Too short escape sequence. Just store it unchanged.
				b = append(b, "\\u"...)
				break
			}
			xs := s[:4]
			x, err := strconv.ParseUint(xs, 16, 16)
			if err != nil {
				// Invalid escape sequence. Just store it unchanged.
				b = append(b, "\\u"...)
				break
			}
			s = s[4:]
			if !utf16.IsSurrogate(rune(x)) {
				b = append(b, string(rune(x))...)
				break
			}

			// Surrogate.
			// See https://en.wikipedia.org/wiki/Universal_Character_Set_characters#Surrogates
			if len(s) < 6 || s[0] != '\\' || s[1] != 'u' {
				b = append(b, "\\u"...)
				b = append(b, xs...)
				break
			}
			x1, err := strconv.ParseUint(s[2:6], 16, 16)
			if err != nil {
				b = append(b, "\\u"...)
				b = append(b, xs...)
				break
			}
			r := utf16.DecodeRune(rune(x), rune(x1))
			b = append(b, string(r)...)
			s = s[6:]
		default:
			// Unknown escape sequence. Just store it unchanged.
			b = append(b, '\\', ch)
		}
		n = strings.IndexByte(s, '\\')
		if n < 0 {
			b = append(b, s...)
			break
		}
		b = append(b, s[:n]...)
		s = s[n+1:]
	}
	return b2s(b)
}

// parseRawKey is similar to parseRawString, but is optimized
// for small-sized keys without escape sequences.
func parseRawKey(s string) (string, string, error) {
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			// Fast path.
			return s[:i], s[i+1:], nil
		}
		if s[i] == '\\' {
			// Slow path.
			return parseRawString(s)
		}
	}
	return s, "", fmt.Errorf(`missing closing '"'`)
}

func parseRawString(s string) (string, string, error) {
	n := strings.IndexByte(s, '"')
	if n < 0 {
		return s, "", fmt.Errorf(`missing closing '"'`)
	}
	if n == 0 || s[n-1] != '\\' {
		// Fast path. No escaped ".
		return s[:n], s[n+1:], nil
	}

	// Slow path - possible escaped " found.
	ss := s
	for {
		i := n - 1
		for i > 0 && s[i-1] == '\\' {
			i--
		}
		if uint(n-i)%2 == 0 {
			return ss[:len(ss)-len(s)+n], s[n+1:], nil
		}
		s = s[n+1:]

		n = strings.IndexByte(s, '"')
		if n < 0 {
			return ss, "", fmt.Errorf(`missing closing '"'`)
		}
		if n == 0 || s[n-1] != '\\' {
			return ss[:len(ss)-len(s)+n], s[n+1:], nil
		}
	}
}

func parseRawNumber(s string) (string, string, error) {
	// The caller must ensure len(s) > 0

	// Find the end of the number.
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == 'e' || ch == 'E' || ch == '+' {
			continue
		}
		if i == 0 || i == 1 && (s[0] == '-' || s[0] == '+') {
			if len(s[i:]) >= 3 {
				xs := s[i : i+3]
				if strings.EqualFold(xs, "inf") || strings.EqualFold(xs, "nan") {
					return s[:i+3], s[i+3:], nil
				}
			}
			return "", s, fmt.Errorf("unexpected char: %q", s[:1])
		}
		ns := s[:i]
		s = s[i:]
		return ns, s, nil
	}
	return s, "", nil
}

type kv struct {
	k string
	v *Value
}

// Object represents JSON object.
//
// Object cannot be used from concurrent goroutines.
// Use per-goroutine parsers or ParserPool instead.
type Object struct {
	kvs           []kv
	keysUnescaped bool
}

func (o *Object) reset() {
	o.kvs = o.kvs[:0]
	o.keysUnescaped = false
}

func (o *Object) getKV() *kv {
	if cap(o.kvs) > len(o.kvs) {
		o.kvs = o.kvs[:len(o.kvs)+1]
	} else {
		o.kvs = append(o.kvs, kv{})
	}
	return &o.kvs[len(o.kvs)-1]
}

func (o *Object) unescapeKeys() {
	if o.keysUnescaped {
		return
	}
	kvs := o.kvs
	for i := range kvs {
		kv := &kvs[i]
		kv.k = unescapeStringBestEffort(kv.k)
	}
	o.keysUnescaped = true
}

// Len returns the number of items in the o.
func (o *Object) Len() int {
	return len(o.kvs)
}

// Get returns the value for the given key in the o.
//
// Returns nil if the value for the given key isn't found.
//
// The returned value is valid until parse is called on the parser returned o.
func (o *Object) Get(key string) *Value {
	if !o.keysUnescaped && strings.IndexByte(key, '\\') < 0 {
		// Fast path - try searching for the key without object keys unescaped.
		for _, kv := range o.kvs {
			if kv.k == key {
				return kv.v
			}
		}
	}

	// Slow path - unescape object keys.
	o.unescapeKeys()

	for _, kv := range o.kvs {
		if kv.k == key {
			return kv.v
		}
	}
	return nil
}

// Visit calls f for each item in the o in the original order
// of the parsed JSON.
//
// f cannot hold key and/or v after returning.
func (o *Object) Visit(f func(key []byte, v *Value)) {
	if o == nil {
		return
	}

	o.unescapeKeys()

	for _, kv := range o.kvs {
		f(s2b(kv.k), kv.v)
	}
}

// MarshalTo appends marshaled o to dst and returns the result.
func (o *Object) MarshalTo(dst []byte) []byte {
	dst = append(dst, '{')
	for i, kv := range o.kvs {
		if o.keysUnescaped {
			dst = escapeString(dst, kv.k)
		} else {
			dst = append(dst, '"')
			dst = append(dst, kv.k...)
			dst = append(dst, '"')
		}
		dst = append(dst, ':')
		dst = kv.v.MarshalTo(dst)
		if i != len(o.kvs)-1 {
			dst = append(dst, ',')
		}
	}
	dst = append(dst, '}')
	return dst
}

// String returns string representation for the o.
//
// This function is for debugging purposes only. It isn't optimized for speed.
// See MarshalTo instead.
func (o *Object) String() string {
	b := o.MarshalTo(nil)
	// It is safe converting b to string without allocation, since b is no longer
	// reachable after this line.
	return b2s(b)
}

// Value represents any JSON value.
//
// Call vType in order to determine the actual type of the JSON value.
//
// Value cannot be used from concurrent goroutines.
// Use per-goroutine parsers or ParserPool instead.
type Value struct {
	o Object
	a []*Value
	s string
	t vType
}

// vType returns the type of the v.
func (v *Value) vType() vType {
	if v.t == typeRawString {
		v.s = unescapeStringBestEffort(v.s)
		v.t = typeString
	}
	return v.t
}

// MarshalTo appends marshaled v to dst and returns the result.
func (v *Value) MarshalTo(dst []byte) []byte {
	switch v.t {
	case typeRawString:
		dst = append(dst, '"')
		dst = append(dst, v.s...)
		dst = append(dst, '"')
		return dst
	case typeObject:
		return v.o.MarshalTo(dst)
	case typeArray:
		dst = append(dst, '[')
		for i, vv := range v.a {
			dst = vv.MarshalTo(dst)
			if i != len(v.a)-1 {
				dst = append(dst, ',')
			}
		}
		dst = append(dst, ']')
		return dst
	case typeString:
		return escapeString(dst, v.s)
	case typeNumber:
		return append(dst, v.s...)
	case typeTrue:
		return append(dst, "true"...)
	case typeFalse:
		return append(dst, "false"...)
	case typeNull:
		return append(dst, "null"...)
	default:
		panic(fmt.Errorf("BUG: unexpected Value type: %d", v.t))
	}
}

// String returns string representation of the v.
//
// The function is for debugging purposes only. It isn't optimized for speed.
// See MarshalTo instead.
//
// Don't confuse this function with StringBytes, which must be called
// for obtaining the underlying JSON string for the v.
func (v *Value) String() string {
	b := v.MarshalTo(nil)
	// It is safe converting b to string without allocation, since b is no longer
	// reachable after this line.
	return b2s(b)
}

// Exists returns true if the field exists for the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
func (v *Value) Exists(keys ...string) bool {
	return v.Get(keys...) != nil
}

// Get returns value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// nil is returned for non-existing keys path.
//
// The returned value is valid until parse is called on the parser returned v.
func (v *Value) Get(keys ...string) *Value {
	if v == nil {
		return nil
	}
	_v := v
	for _, key := range keys {
		if _v.t == typeObject {
			_v = _v.o.Get(key)
			if _v == nil {
				return nil
			}
		} else if _v.t == typeArray {
			n, err := strconv.Atoi(key)
			if err != nil || n < 0 || n >= len(_v.a) {
				return nil
			}
			_v = _v.a[n]
		} else {
			return nil
		}
	}
	return _v
}

// GetObject returns object value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// nil is returned for non-existing keys path or for invalid value type.
//
// The returned object is valid until parse is called on the parser returned v.
func (v *Value) GetObject(keys ...string) *Object {
	r := v.Get(keys...)
	if r == nil || r.t != typeObject {
		return nil
	}
	return &r.o
}

// GetArray returns array value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// nil is returned for non-existing keys path or for invalid value type.
//
// The returned array is valid until parse is called on the parser returned v.
func (v *Value) GetArray(keys ...string) []*Value {
	r := v.Get(keys...)
	if r == nil || r.t != typeArray {
		return nil
	}
	return r.a
}

// GetFloat64 returns float64 value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// 0 is returned for non-existing keys path or for invalid value type.
func (v *Value) GetFloat64(keys ...string) float64 {
	r := v.Get(keys...)
	if r == nil || r.vType() != typeNumber {
		return 0
	}
	return parseBestEffort(r.s)
}

// GetInt returns int value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// 0 is returned for non-existing keys path or for invalid value type.
func (v *Value) GetInt(keys ...string) int {
	r := v.Get(keys...)
	if r == nil || r.vType() != typeNumber {
		return 0
	}
	n := parseInt64BestEffort(r.s)
	nn := int(n)
	if int64(nn) != n {
		return 0
	}
	return nn
}

// GetUint returns uint value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// 0 is returned for non-existing keys path or for invalid value type.
func (v *Value) GetUint(keys ...string) uint {
	r := v.Get(keys...)
	if r == nil || r.vType() != typeNumber {
		return 0
	}
	n := parseUint64BestEffort(r.s)
	nn := uint(n)
	if uint64(nn) != n {
		return 0
	}
	return nn
}

// GetInt64 returns int64 value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// 0 is returned for non-existing keys path or for invalid value type.
func (v *Value) GetInt64(keys ...string) int64 {
	r := v.Get(keys...)
	if r == nil || r.vType() != typeNumber {
		return 0
	}
	return parseInt64BestEffort(r.s)
}

// GetUint64 returns uint64 value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// 0 is returned for non-existing keys path or for invalid value type.
func (v *Value) GetUint64(keys ...string) uint64 {
	r := v.Get(keys...)
	if r == nil || r.vType() != typeNumber {
		return 0
	}
	return parseUint64BestEffort(r.s)
}

func (v *Value) GetString(keys ...string) string {
	r := v.Get(keys...)
	if r == nil || r.vType() != typeString {
		return ""
	}
	return r.s
}

// GetStringBytes returns string value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// nil is returned for non-existing keys path or for invalid value type.
//
// The returned string is valid until parse is called on the parser returned v.
func (v *Value) GetStringBytes(keys ...string) []byte {
	r := v.Get(keys...)
	if r == nil || r.vType() != typeString {
		return nil
	}
	return s2b(r.s)
}

// GetBool returns bool value by the given keys path.
//
// Array indexes may be represented as decimal numbers in keys.
//
// false is returned for non-existing keys path or for invalid value type.
func (v *Value) GetBool(keys ...string) bool {
	r := v.Get(keys...)
	if r != nil && r.t == typeTrue {
		return true
	}
	return false
}

// Object returns the underlying JSON object for the v.
//
// The returned object is valid until parse is called on the parser returned v.
//
// Use GetObject if you don't need error handling.
func (v *Value) Object() (*Object, error) {
	if v.t != typeObject {
		return nil, fmt.Errorf("value doesn't contain object; it contains %s", v.vType())
	}
	return &v.o, nil
}

// Array returns the underlying JSON array for the v.
//
// The returned array is valid until parse is called on the parser returned v.
//
// Use GetArray if you don't need error handling.
func (v *Value) Array() ([]*Value, error) {
	if v.t != typeArray {
		return nil, fmt.Errorf("value doesn't contain array; it contains %s", v.vType())
	}
	return v.a, nil
}

// StringBytes returns the underlying JSON string for the v.
//
// The returned string is valid until parse is called on the parser returned v.
//
// Use GetStringBytes if you don't need error handling.
func (v *Value) StringBytes() ([]byte, error) {
	if v.vType() != typeString {
		return nil, fmt.Errorf("value doesn't contain string; it contains %s", v.vType())
	}
	return s2b(v.s), nil
}

// Float64 returns the underlying JSON number for the v.
//
// Use GetFloat64 if you don't need error handling.
func (v *Value) Float64() (float64, error) {
	if v.vType() != typeNumber {
		return 0, fmt.Errorf("value doesn't contain number; it contains %s", v.vType())
	}
	return parse(v.s)
}

// Int returns the underlying JSON int for the v.
//
// Use GetInt if you don't need error handling.
func (v *Value) Int() (int, error) {
	if v.vType() != typeNumber {
		return 0, fmt.Errorf("value doesn't contain number; it contains %s", v.vType())
	}
	n, err := parseInt64(v.s)
	if err != nil {
		return 0, err
	}
	nn := int(n)
	if int64(nn) != n {
		return 0, fmt.Errorf("number %q doesn't fit int", v.s)
	}
	return nn, nil
}

// Uint returns the underlying JSON uint for the v.
//
// Use GetInt if you don't need error handling.
func (v *Value) Uint() (uint, error) {
	if v.vType() != typeNumber {
		return 0, fmt.Errorf("value doesn't contain number; it contains %s", v.vType())
	}
	n, err := parseUint64(v.s)
	if err != nil {
		return 0, err
	}
	nn := uint(n)
	if uint64(nn) != n {
		return 0, fmt.Errorf("number %q doesn't fit uint", v.s)
	}
	return nn, nil
}

// Int64 returns the underlying JSON int64 for the v.
//
// Use GetInt64 if you don't need error handling.
func (v *Value) Int64() (int64, error) {
	if v.vType() != typeNumber {
		return 0, fmt.Errorf("value doesn't contain number; it contains %s", v.vType())
	}
	return parseInt64(v.s)
}

// Uint64 returns the underlying JSON uint64 for the v.
//
// Use GetInt64 if you don't need error handling.
func (v *Value) Uint64() (uint64, error) {
	if v.vType() != typeNumber {
		return 0, fmt.Errorf("value doesn't contain number; it contains %s", v.vType())
	}
	return parseUint64(v.s)
}

// Bool returns the underlying JSON bool for the v.
//
// Use GetBool if you don't need error handling.
func (v *Value) Bool() (bool, error) {
	if v.t == typeTrue {
		return true, nil
	}
	if v.t == typeFalse {
		return false, nil
	}
	return false, fmt.Errorf("value doesn't contain bool; it contains %s", v.vType())
}

var (
	valueTrue  = &Value{t: typeTrue}
	valueFalse = &Value{t: typeFalse}
	valueNull  = &Value{t: typeNull}
)

// parseBestEffort parses floating-point number s.
//
// It is equivalent to strconv.ParseFloat(s, 64), but is faster.
//
// 0 is returned if the number cannot be parsed.
// See also parse, which returns parse error if the number cannot be parsed.
func parseBestEffort(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	i := uint(0)
	minus := s[0] == '-'
	if minus {
		i++
		if i >= uint(len(s)) {
			return 0
		}
	}

	// the integer part might be elided to remain compliant
	// with https://go.dev/ref/spec#Floating-point_literals
	if s[i] == '.' && (i+1 >= uint(len(s)) || s[i+1] < '0' || s[i+1] > '9') {
		return 0
	}

	d := uint64(0)
	j := i
	for i < uint(len(s)) {
		if s[i] >= '0' && s[i] <= '9' {
			d = d*10 + uint64(s[i]-'0')
			i++
			if i > 18 {
				// The integer part may be out of range for uint64.
				// Fall back to slow parsing.
				f, err := strconv.ParseFloat(s, 64)
				if err != nil && !math.IsInf(f, 0) {
					return 0
				}
				return f
			}
			continue
		}
		break
	}
	if i <= j && s[i] != '.' {
		s = s[i:]
		if strings.HasPrefix(s, "+") {
			s = s[1:]
		}
		// "infinity" is needed for OpenMetrics support.
		// See https://github.com/OpenObservability/OpenMetrics/blob/master/OpenMetrics.md
		if strings.EqualFold(s, "inf") || strings.EqualFold(s, "infinity") {
			if minus {
				return -inf
			}
			return inf
		}
		if strings.EqualFold(s, "nan") {
			return nan
		}
		return 0
	}
	f := float64(d)
	if i >= uint(len(s)) {
		// Fast path - just integer.
		if minus {
			f = -f
		}
		return f
	}

	if s[i] == '.' {
		// parse fractional part.
		i++
		if i >= uint(len(s)) {
			// the fractional part may be elided to remain compliant
			// with https://go.dev/ref/spec#Floating-point_literals
			return f
		}
		k := i
		for i < uint(len(s)) {
			if s[i] >= '0' && s[i] <= '9' {
				d = d*10 + uint64(s[i]-'0')
				i++
				if i-j >= uint(len(float64pow10)) {
					// The mantissa is out of range. Fall back to standard parsing.
					f, err := strconv.ParseFloat(s, 64)
					if err != nil && !math.IsInf(f, 0) {
						return 0
					}
					return f
				}
				continue
			}
			break
		}
		if i < k {
			return 0
		}
		// Convert the entire mantissa to a float at once to avoid rounding errors.
		f = float64(d) / float64pow10[i-k]
		if i >= uint(len(s)) {
			// Fast path - parsed fractional number.
			if minus {
				f = -f
			}
			return f
		}
	}
	if s[i] == 'e' || s[i] == 'E' {
		// parse exponent part.
		i++
		if i >= uint(len(s)) {
			return 0
		}
		expMinus := false
		if s[i] == '+' || s[i] == '-' {
			expMinus = s[i] == '-'
			i++
			if i >= uint(len(s)) {
				return 0
			}
		}
		exp := int16(0)
		j := i
		for i < uint(len(s)) {
			if s[i] >= '0' && s[i] <= '9' {
				exp = exp*10 + int16(s[i]-'0')
				i++
				if exp > 300 {
					// The exponent may be too big for float64.
					// Fall back to standard parsing.
					f, err := strconv.ParseFloat(s, 64)
					if err != nil && !math.IsInf(f, 0) {
						return 0
					}
					return f
				}
				continue
			}
			break
		}
		if i <= j {
			return 0
		}
		if expMinus {
			exp = -exp
		}
		f *= math.Pow10(int(exp))
		if i >= uint(len(s)) {
			if minus {
				f = -f
			}
			return f
		}
	}
	return 0
}

// parseInt64BestEffort parses int64 number s.
//
// It is equivalent to strconv.ParseInt(s, 10, 64), but is faster.
//
// 0 is returned if the number cannot be parsed.
// See also parseInt64, which returns parse error if the number cannot be parsed.
func parseInt64BestEffort(s string) int64 {
	if len(s) == 0 {
		return 0
	}
	i := uint(0)
	minus := s[0] == '-'
	if minus {
		i++
		if i >= uint(len(s)) {
			return 0
		}
	}

	d := int64(0)
	j := i
	for i < uint(len(s)) {
		if s[i] >= '0' && s[i] <= '9' {
			d = d*10 + int64(s[i]-'0')
			i++
			if i > 18 {
				// The integer part may be out of range for int64.
				// Fall back to slow parsing.
				dd, err := strconv.ParseInt(s, 10, 64)
				if err != nil {
					return 0
				}
				return dd
			}
			continue
		}
		break
	}
	if i <= j {
		return 0
	}
	if i < uint(len(s)) {
		// Unparsed tail left.
		return 0
	}
	if minus {
		d = -d
	}
	return d
}

// parseUint64BestEffort parses uint64 number s.
//
// It is equivalent to strconv.ParseUint(s, 10, 64), but is faster.
//
// 0 is returned if the number cannot be parsed.
// See also parseUint64, which returns parse error if the number cannot be parsed.
func parseUint64BestEffort(s string) uint64 {
	if len(s) == 0 {
		return 0
	}
	i := uint(0)
	d := uint64(0)
	j := i
	for i < uint(len(s)) {
		if s[i] >= '0' && s[i] <= '9' {
			d = d*10 + uint64(s[i]-'0')
			i++
			if i > 18 {
				// The integer part may be out of range for uint64.
				// Fall back to slow parsing.
				dd, err := strconv.ParseUint(s, 10, 64)
				if err != nil {
					return 0
				}
				return dd
			}
			continue
		}
		break
	}
	if i <= j {
		return 0
	}
	if i < uint(len(s)) {
		// Unparsed tail left.
		return 0
	}
	return d
}

// parse parses floating-point number s.
//
// It is equivalent to strconv.ParseFloat(s, 64), but is faster.
//
// See also parseBestEffort.
func parse(s string) (float64, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("cannot parse float64 from empty string")
	}
	i := uint(0)
	minus := s[0] == '-'
	if minus {
		i++
		if i >= uint(len(s)) {
			return 0, fmt.Errorf("cannot parse float64 from %q", s)
		}
	}

	// the integer part might be elided to remain compliant
	// with https://go.dev/ref/spec#Floating-point_literals
	if s[i] == '.' && (i+1 >= uint(len(s)) || s[i+1] < '0' || s[i+1] > '9') {
		return 0, fmt.Errorf("missing integer and fractional part in %q", s)
	}

	d := uint64(0)
	j := i
	for i < uint(len(s)) {
		if s[i] >= '0' && s[i] <= '9' {
			d = d*10 + uint64(s[i]-'0')
			i++
			if i > 18 {
				// The integer part may be out of range for uint64.
				// Fall back to slow parsing.
				f, err := strconv.ParseFloat(s, 64)
				if err != nil && !math.IsInf(f, 0) {
					return 0, err
				}
				return f, nil
			}
			continue
		}
		break
	}
	if i <= j && s[i] != '.' {
		ss := s[i:]
		if strings.HasPrefix(ss, "+") {
			ss = ss[1:]
		}
		// "infinity" is needed for OpenMetrics support.
		// See https://github.com/OpenObservability/OpenMetrics/blob/master/OpenMetrics.md
		if strings.EqualFold(ss, "inf") || strings.EqualFold(ss, "infinity") {
			if minus {
				return -inf, nil
			}
			return inf, nil
		}
		if strings.EqualFold(ss, "nan") {
			return nan, nil
		}
		return 0, fmt.Errorf("unparsed tail left after parsing float64 from %q: %q", s, ss)
	}
	f := float64(d)
	if i >= uint(len(s)) {
		// Fast path - just integer.
		if minus {
			f = -f
		}
		return f, nil
	}

	if s[i] == '.' {
		// parse fractional part.
		i++
		if i >= uint(len(s)) {
			// the fractional part might be elided to remain compliant
			// with https://go.dev/ref/spec#Floating-point_literals
			return f, nil
		}
		k := i
		for i < uint(len(s)) {
			if s[i] >= '0' && s[i] <= '9' {
				d = d*10 + uint64(s[i]-'0')
				i++
				if i-j >= uint(len(float64pow10)) {
					// The mantissa is out of range. Fall back to standard parsing.
					f, err := strconv.ParseFloat(s, 64)
					if err != nil && !math.IsInf(f, 0) {
						return 0, fmt.Errorf("cannot parse mantissa in %q: %s", s, err)
					}
					return f, nil
				}
				continue
			}
			break
		}
		if i < k {
			return 0, fmt.Errorf("cannot find mantissa in %q", s)
		}
		// Convert the entire mantissa to a float at once to avoid rounding errors.
		f = float64(d) / float64pow10[i-k]
		if i >= uint(len(s)) {
			// Fast path - parsed fractional number.
			if minus {
				f = -f
			}
			return f, nil
		}
	}
	if s[i] == 'e' || s[i] == 'E' {
		// parse exponent part.
		i++
		if i >= uint(len(s)) {
			return 0, fmt.Errorf("cannot parse exponent in %q", s)
		}
		expMinus := false
		if s[i] == '+' || s[i] == '-' {
			expMinus = s[i] == '-'
			i++
			if i >= uint(len(s)) {
				return 0, fmt.Errorf("cannot parse exponent in %q", s)
			}
		}
		exp := int16(0)
		j := i
		for i < uint(len(s)) {
			if s[i] >= '0' && s[i] <= '9' {
				exp = exp*10 + int16(s[i]-'0')
				i++
				if exp > 300 {
					// The exponent may be too big for float64.
					// Fall back to standard parsing.
					f, err := strconv.ParseFloat(s, 64)
					if err != nil && !math.IsInf(f, 0) {
						return 0, fmt.Errorf("cannot parse exponent in %q: %s", s, err)
					}
					return f, nil
				}
				continue
			}
			break
		}
		if i <= j {
			return 0, fmt.Errorf("cannot parse exponent in %q", s)
		}
		if expMinus {
			exp = -exp
		}
		f *= math.Pow10(int(exp))
		if i >= uint(len(s)) {
			if minus {
				f = -f
			}
			return f, nil
		}
	}
	return 0, fmt.Errorf("cannot parse float64 from %q", s)
}

// parseInt64 parses int64 number s.
//
// It is equivalent to strconv.ParseInt(s, 10, 64), but is faster.
//
// See also parseInt64BestEffort.
func parseInt64(s string) (int64, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("cannot parse int64 from empty string")
	}
	i := uint(0)
	minus := s[0] == '-'
	if minus {
		i++
		if i >= uint(len(s)) {
			return 0, fmt.Errorf("cannot parse int64 from %q", s)
		}
	}

	d := int64(0)
	j := i
	for i < uint(len(s)) {
		if s[i] >= '0' && s[i] <= '9' {
			d = d*10 + int64(s[i]-'0')
			i++
			if i > 18 {
				// The integer part may be out of range for int64.
				// Fall back to slow parsing.
				dd, err := strconv.ParseInt(s, 10, 64)
				if err != nil {
					return 0, err
				}
				return dd, nil
			}
			continue
		}
		break
	}
	if i <= j {
		return 0, fmt.Errorf("cannot parse int64 from %q", s)
	}
	if i < uint(len(s)) {
		// Unparsed tail left.
		return 0, fmt.Errorf("unparsed tail left after parsing int64 form %q: %q", s, s[i:])
	}
	if minus {
		d = -d
	}
	return d, nil
}

// parseUint64 parses uint64 from s.
//
// It is equivalent to strconv.ParseUint(s, 10, 64), but is faster.
//
// See also parseUint64BestEffort.
func parseUint64(s string) (uint64, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("cannot parse uint64 from empty string")
	}
	i := uint(0)
	d := uint64(0)
	j := i
	for i < uint(len(s)) {
		if s[i] >= '0' && s[i] <= '9' {
			d = d*10 + uint64(s[i]-'0')
			i++
			if i > 18 {
				// The integer part may be out of range for uint64.
				// Fall back to slow parsing.
				dd, err := strconv.ParseUint(s, 10, 64)
				if err != nil {
					return 0, err
				}
				return dd, nil
			}
			continue
		}
		break
	}
	if i <= j {
		return 0, fmt.Errorf("cannot parse uint64 from %q", s)
	}
	if i < uint(len(s)) {
		// Unparsed tail left.
		return 0, fmt.Errorf("unparsed tail left after parsing uint64 from %q: %q", s, s[i:])
	}
	return d, nil
}

const maxStartEndStringLen = 80

func startEndString(s string) string {
	if len(s) <= maxStartEndStringLen {
		return s
	}
	start := s[:40]
	end := s[len(s)-40:]
	return start + "..." + end
}

func skipWS(s string) string {
	if len(s) == 0 || s[0] > 0x20 {
		// Fast path.
		return s
	}
	return skipWSSlow(s)
}

func skipWSSlow(s string) string {
	if len(s) == 0 || s[0] != 0x20 && s[0] != 0x0A && s[0] != 0x09 && s[0] != 0x0D {
		return s
	}
	for i := 1; i < len(s); i++ {
		if s[i] != 0x20 && s[i] != 0x0A && s[i] != 0x09 && s[i] != 0x0D {
			return s[i:]
		}
	}
	return ""
}

func b2s(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

func s2b(s string) (b []byte) {
	strH := (*reflect.StringHeader)(unsafe.Pointer(&s))
	sh := (*reflect.SliceHeader)(unsafe.Pointer(&b))
	sh.Data = strH.Data
	sh.Len = strH.Len
	sh.Cap = strH.Len
	return b
}

// Exact powers of 10.
//
// This works faster than math.Pow10, since it avoids additional multiplication.
var float64pow10 = [...]float64{
	1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16,
}

var inf = math.Inf(1)
var nan = math.NaN()
