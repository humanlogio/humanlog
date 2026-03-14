package flatjson

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type EntityType uint8

const (
	EntityType_Invalid = iota
	EntityType_String
	EntityType_Object
	EntityType_Array
	EntityType_Number
	EntityType_Boolean_True
	EntityType_Boolean_False
	EntityType_Null
)

func GuessNextEntityType(data []byte, i int) EntityType {
	// decide if the value is a number, string, object, array, bool or null
	b := data[i]
	if b == '"' { // strings
		return EntityType_String
	} else if b == '{' { // objects
		return EntityType_Object
	} else if b == '[' { // arrays
		return EntityType_Array
	} else if b == '-' || (b >= '0' && b <= '9') { // numbers
		return EntityType_Number
	} else if i+3 < len(data) && // bool - true case
		b == 't' &&
		data[i+1] == 'r' &&
		data[i+2] == 'u' &&
		data[i+3] == 'e' {
		return EntityType_Boolean_True
	} else if i+4 < len(data) && // bool - false case
		b == 'f' &&
		data[i+1] == 'a' &&
		data[i+2] == 'l' &&
		data[i+3] == 's' &&
		data[i+4] == 'e' {
		return EntityType_Boolean_False

	} else if i+3 < len(data) && // null
		b == 'n' &&
		data[i+1] == 'u' &&
		data[i+2] == 'l' &&
		data[i+3] == 'l' {
		return EntityType_Null
	}
	return EntityType_Invalid
}

type SyntaxError struct {
	Offset  int
	Message string

	SubErr *SyntaxError
}

func syntaxErr(offset int, msg string, suberr *SyntaxError) *SyntaxError {
	return &SyntaxError{
		Offset:  offset,
		Message: msg,
		SubErr:  suberr,
	}
}

func (s *SyntaxError) Error() string {
	if s.SubErr == nil {
		return s.Message
	}
	return s.Message + ", " + s.SubErr.Error()
}

type Pos struct {
	From int
	To   int
}

func (p Pos) Bytes(data []byte) []byte  { return data[p.From:p.To] }
func (p Pos) String(data []byte) string { return string(p.Bytes(data)) }

type Prefixes []Prefix

func (pfxs Prefixes) AsString(data []byte) string {
	if len(pfxs) == 0 {
		return ""
	}
	bd := strings.Builder{}
	for i, pfx := range pfxs {
		if i != 0 {
			bd.WriteRune('.')
		}
		if pfx.IsArrayIndex() {
			bd.WriteString(strconv.Itoa(pfx.Index()))
		} else {
			s, err := Unquote(pfx.Bytes(data))
			if err != nil {
				panic(fmt.Sprintf("prefix %q: %v", pfx.String(data), err))
			}
			bd.Write(s)
		}
	}
	return bd.String()
}

type Prefix struct {
	from int
	to   int
}

func newObjectKeyPrefix(from, to int) Prefix {
	return Prefix{from: from, to: to}
}

func newArrayIndexPrefix(index int) Prefix {
	return Prefix{from: index, to: 0}
}

func (pfx Prefix) IsObjectKey() bool {
	return pfx.to != 0
}

func (pfx Prefix) IsArrayIndex() bool {
	return pfx.to == 0
}

func (pfx Prefix) Bytes(data []byte) []byte {
	if pfx.IsArrayIndex() {
		return []byte(strconv.Itoa(pfx.from))
	}
	return data[pfx.from:pfx.to]
}
func (pfx Prefix) String(data []byte) string { return string(pfx.Bytes(data)) }
func (pfx Prefix) Index() int                { return pfx.from }

type Float struct {
	Name  Prefix
	Value float64
}

type Integer struct {
	Name  Prefix
	Value int64
}

type String struct {
	Name  Prefix
	Value Pos
}

type Bool struct {
	Name  Prefix
	Value bool
}

type Null struct{ Name Prefix }

type (
	FloatDec   func(prefixes Prefixes, val Float)
	IntegerDec func(prefixes Prefixes, val Integer)
	StringDec  func(prefixes Prefixes, val String)
	BooleanDec func(prefixes Prefixes, val Bool)
	NullDec    func(prefixes Prefixes, val Null)

	// TODO: not supported yet
	objectDec func(prefixes Prefixes, name Prefix, cb *Callbacks)
	arrayDec  func(prefixes Prefixes, cb *Callbacks)
)

type Callbacks struct {
	MaxDepth int

	OnFloat   FloatDec
	OnInteger IntegerDec
	OnString  StringDec
	OnBoolean BooleanDec
	OnNull    NullDec

	// TODO: not supported yet
	onObject objectDec
	onArray  arrayDec

	OnRaw func(prefixes Prefixes, name Prefix, value Pos)
}

const (
	noOpeningBracketFound           = "doesn't begin with a `{`"
	endOfDataNoNamePair             = "end of data reached searching a name for a name/value pair"
	expectingNameBeforeValue        = "expecting a name before a value"
	endOfDataNoColon                = "end of data reached searching a colon between a name/value pair"
	noColonFound                    = "expecting a colon between names and values"
	endOfDataNoValueForName         = "end of data reached searching a value for a name/value pair"
	beginNumberValueButError        = "found beginning of a number value"
	beginStringValueButError        = "found beginning of a string value"
	expectValueButNoKnownType       = "expected value, but was neither an object, array, number, string, bool or null"
	endOfDataNoClosingBracket       = "end of data reached and end of object not found"
	endOfDataNoClosingSquareBracket = "end of data reached and end of array not found"
	malformedNumber                 = "number value in name/value pair is malformed"

	noOpeningSquareBracketFound = "doesn't begin with a `[`"
	beginObjectValueButError    = "found beginning of an object value"
	beginArrayValueButError     = "found beginning of an array value"
	endOfDataNoValue            = "end of data reached searching a value"
)

// ScanObject according to the spec at http://www.json.org/
// but ignoring nested objects and arrays
func ScanObject(data []byte, from int, cb *Callbacks) (pos Pos, found bool, err error) {
	return scanObject(data, from, nil, cb)
}

func scanObject(data []byte, from int, prefixes []Prefix, cb *Callbacks) (pos Pos, found bool, _ error) {
	if from < 0 {
		panic(fmt.Sprintf("negative starting index %d", from))
	} else if len(data) == 0 {
		return Pos{}, false, nil
	} else if from >= len(data) {
		panic(fmt.Sprintf("starting index %d is larger than provided data len(%d)", from, len(data)))
	}
	pos.From, pos.To = -1, -1
	start := skipWhitespace(data, from)
	if len(data) == start {
		return Pos{0, start}, false, nil
	}
	if len(data) == 0 || data[start] != '{' {
		return pos, false, syntaxErr(start, noOpeningBracketFound, nil)
	}
	i := start + 1
	for ; i < len(data); i++ {

		i = skipWhitespace(data, i)
		if i >= len(data) {
			return pos, false, syntaxErr(i, endOfDataNoNamePair, nil)
		}

		if data[i] == '}' {
			return Pos{start, i + 1}, true, nil
		}

		// scan the name
		pfx, j, err := scanPairName(data, i)
		if err != nil {
			return Pos{From: pfx.from, To: pfx.to}, false, err
		}
		i = j

		// decide if the value is a number, string, object, array, bool or null
		et := GuessNextEntityType(data, i)

		var valPos Pos
		if et == EntityType_String { // strings
			valPos, err = scanString(data, i)
			if err != nil {
				return pos, false, syntaxErr(i, beginStringValueButError, err.(*SyntaxError))
			}

			if cb != nil && cb.OnString != nil && cb.MaxDepth >= len(prefixes) {
				cb.OnString(prefixes, String{Name: pfx, Value: valPos})
			}
			i = valPos.To

		} else if et == EntityType_Object { // objects
			// careful not to shadow `valPos`, we need it to be updated
			valPos, found, err = scanObject(data, i, append(prefixes, pfx), cb) // TODO: fix recursion
			if err != nil {
				return Pos{}, found, syntaxErr(i, beginObjectValueButError, err.(*SyntaxError))
			} else if !found {
				return Pos{}, found, syntaxErr(i, expectValueButNoKnownType, nil)
			}
			i = valPos.To

		} else if et == EntityType_Array { // arrays
			// careful not to shadow `valPos`, we need it to be updated
			valPos, found, err = scanArray(data, i, append(prefixes, pfx), cb) // TODO: fix recursion
			if err != nil {
				return Pos{}, found, syntaxErr(i, beginArrayValueButError, err.(*SyntaxError))
			} else if !found {
				return Pos{}, found, syntaxErr(i, expectValueButNoKnownType, nil)
			}
			i = valPos.To

		} else if et == EntityType_Number { // numbers
			f64, i64, isInt, j, err := scanNumber(data, i)
			if err != nil {
				return pos, false, syntaxErr(i, beginNumberValueButError, err.(*SyntaxError))
			}
			valPos = Pos{From: i, To: j}
			j = skipWhitespace(data, j)
			if j < len(data) && data[j] != ',' && data[j] != '}' {
				return pos, false, syntaxErr(i, malformedNumber, nil)
			}
			if cb != nil && cb.MaxDepth >= len(prefixes) {
				if isInt && cb.OnInteger != nil {
					cb.OnInteger(prefixes, Integer{Name: pfx, Value: i64})
				} else if cb.OnFloat != nil {
					cb.OnFloat(prefixes, Float{Name: pfx, Value: f64})
				}
			}
			i = j

		} else if et == EntityType_Boolean_True {
			j = i + 4
			valPos = Pos{From: i, To: j}
			if cb != nil && cb.OnBoolean != nil && cb.MaxDepth >= len(prefixes) {
				cb.OnBoolean(prefixes, Bool{Name: pfx, Value: true})
			}
			i = j

		} else if et == EntityType_Boolean_False {
			j = i + 5
			valPos = Pos{From: i, To: j}
			if cb != nil && cb.OnBoolean != nil && cb.MaxDepth >= len(prefixes) {
				cb.OnBoolean(prefixes, Bool{Name: pfx, Value: false})
			}
			i = j

		} else if et == EntityType_Null {
			j = i + 4
			if cb != nil && cb.OnNull != nil && cb.MaxDepth >= len(prefixes) {
				cb.OnNull(prefixes, Null{Name: pfx})
			}
			valPos = Pos{From: i, To: j}
			i = j

		} else {
			return pos, false, syntaxErr(i, expectValueButNoKnownType, nil)
		}
		if cb != nil && cb.OnRaw != nil && cb.MaxDepth >= len(prefixes) {
			cb.OnRaw(prefixes, pfx, valPos)
		}

		i = skipWhitespace(data, i)
		if i < len(data) {
			if data[i] == ',' {
				// more values to come
				// TODO(antoine): be kind and accept trailing commas
			} else if data[i] == '}' {
				return Pos{start, i + 1}, true, nil
			}
		}
	}
	return pos, false, syntaxErr(i, endOfDataNoClosingBracket, nil)
}

const (
	reachedEndScanningCharacters = "reached end of data looking for end of string"
	unicodeNotFollowHex          = "unicode escape code is followed by non-hex characters"
)

// scanString reads a JSON string *position* in data. the `To` position
// is one-past where it last found a string component.
// It does not deal with whitespace.
func scanString(data []byte, i int) (Pos, error) {
	from := i
	to := i + 1
	for ; to < len(data); to++ {
		b := data[to]
		if b == '"' {
			to++
			return Pos{From: from, To: to}, nil
		}
		if b == '\\' {
			// skip:
			//   "
			//   \
			//   /
			//   b
			//   f
			//   n
			//   r
			//   t
			//   u
			to++
			// skip the 4 next hex digits
			if to < len(data) && data[to] == 'u' {
				if len(data) < to+5 {
					return Pos{}, syntaxErr(to, reachedEndScanningCharacters, nil)
				}
				for j, b := range data[to+1 : to+5] {
					if b < '0' ||
						(b > '9' && b < 'A') ||
						(b > 'F' && b < 'a') ||
						(b > 'f') {
						return Pos{}, syntaxErr(to+j-2, unicodeNotFollowHex, nil)
					}
					to++
				}

			}
		}
	}
	return Pos{}, syntaxErr(to-1, reachedEndScanningCharacters, nil)
}

const (
	reachedEndScanningNumber = "reached end of data scanning a number"
	cantFindIntegerPart      = "could not find an integer part"
	scanningForFraction      = "scanning for a fraction"
	scanningForExponent      = "scanning for an exponent"
	scanningForExponentSign  = "scanning for an exponent's sign"
)

// ScanNumber reads a JSON number value from data and advances i one past
// the last number component it found. It does not deal with whitespace.
func ScanNumber(data []byte, i int) (_ float64, _ int64, isInt bool, _ int, _ error) {
	return scanNumber(data, i)
}
func scanNumber(data []byte, i int) (_ float64, _ int64, isInt bool, _ int, _ error) {
	if i >= len(data) {
		return 0, 0, false, i, syntaxErr(i, reachedEndScanningNumber, nil)
	}

	isNeg := false
	if data[i] == '-' {
		isNeg = true
		i++
	}

	if i >= len(data) {
		return 0, 0, false, i, syntaxErr(i, reachedEndScanningNumber, nil)
	}

	var i64 int64
	var f64 float64
	var err error

	isInt = true
	digit := data[i]
	if digit == '0' {
		i++
	} else if digit >= '1' && digit <= '9' {
		var ok bool
		i64, i, ok, err = scanAsI64(data, i)
		if err != nil {
			return 0, 0, false, i, err
		}
		if !ok {
			isInt = false
			f64, i, err = resumeScanAsF64(data, i, float64(i64))
			i64 = 0
		}
	} else {
		err = syntaxErr(i, cantFindIntegerPart, nil)
	}

	if err != nil || i >= len(data) {
		if isNeg {
			i64 = -i64
			f64 = -f64
		}
		return f64, i64, isInt, i, err
	}

	// scan fraction
	if data[i] == '.' {
		if isInt {
			f64 = float64(i64)
		}
		isInt = false
		i64 = 0
		i++
		var frac float64
		frac, i, err = scanFractionalPart(data, i)
		if err != nil {
			return f64, i64, isInt, i, syntaxErr(i, scanningForFraction, err.(*SyntaxError))
		}
		f64 += frac
	}

	if i >= len(data) {
		if isNeg {
			i64 = -i64
			f64 = -f64
		}
		return f64, i64, isInt, i, nil
	}

	if isInt {
		f64 = float64(i64)
	}

	// scan an exponent
	if data[i] == 'e' || data[i] == 'E' {
		i++
		if i >= len(data) {
			return f64, i64, isInt, i, syntaxErr(i, scanningForExponentSign, nil)
		}
		b := data[i]

		// check the sign
		isNegExp := false
		if b == '-' {
			isNegExp = true
			i++
		} else if b == '+' {
			i++
		}

		// find the exponent
		var exp int64
		exp, i, _, err = scanAsI64(data, i)
		if err != nil {
			return f64, i64, isInt, i, syntaxErr(i, scanningForExponent, err.(*SyntaxError))
		}
		expf64 := float64(exp)
		// scale up or down the value
		if isNegExp {
			f64 /= math.Pow(10.0, expf64)
			if isInt && f64 == float64(int64(f64)) {
				// still an integer
				i64 /= intPow(10, exp)
				isInt = true
			} else {
				isInt = false
				i64 = 0
			}
		} else {
			f64 *= math.Pow(10.0, expf64)
			if isInt && f64 >= float64(math.MaxInt64) {
				isInt = false
				i64 = 0 // would overflow
			} else {
				i64 *= intPow(10, exp)
			}
		}
	}
	if isNeg {
		i64 = -i64
		f64 = -f64
	}
	return f64, i64, isInt, i, nil
}

func intPow(n, m int64) int64 {
	if m == 0 {
		return 1
	}

	if m == 1 {
		return n
	}

	result := n
	for i := int64(2); i <= m; i++ {
		result *= n
	}
	return result
}

const (
	needAtLeastOneDigit     = "need at least one digit"
	reachedEndScanningDigit = "reached end of data scanning digits"
)

// scanDigits reads an integer value from data and advances i one-past
// the last digit component of data.
// it does not deal with whitespace
func scanDigits(data []byte, i int) (int64, int, error) {
	// digits := (digit | digit digits)

	if i >= len(data) {
		return 0, i, syntaxErr(i, reachedEndScanningDigit, nil)
	}

	// scan one digit
	b := data[i]
	if b < '0' || b > '9' {
		return 0, i, syntaxErr(i, needAtLeastOneDigit, nil)
	}
	v := int64(b - '0')

	// that might be it
	i++
	if i >= len(data) {
		return v, i, nil
	}

	// scan one or many digits
	for j, b := range data[i:] {
		if b < '0' || b > '9' {
			return v, i + j, nil
		}
		ival := int64(b - '0')
		v *= 10
		v += ival
	}

	i = len(data)
	return v, i, nil
}

func scanAsI64(data []byte, i int) (int64, int, bool, error) {
	// Attempts to scan digits as int64. Returns (value, new index, ok, error).
	// ok=false if overflow occurs or no digits found.
	if i >= len(data) {
		return 0, i, false, syntaxErr(i, reachedEndScanningDigit, nil)
	}
	b := data[i]
	if b < '0' || b > '9' {
		return 0, i, false, syntaxErr(i, needAtLeastOneDigit, nil)
	}
	v := int64(b - '0')
	maxDiv10 := int64(math.MaxInt64 / 10)
	maxMod10 := int64(math.MaxInt64 % 10)
	i++
	for ; i < len(data); i++ {
		b = data[i]
		if b < '0' || b > '9' {
			return v, i, true, nil
		}
		d := int64(b - '0')
		if v > maxDiv10 || (v == maxDiv10 && d > maxMod10) {
			// overflow
			return v, i, false, nil
		}
		v = v*10 + d
	}
	return v, i, true, nil
}

func resumeScanAsF64(data []byte, i int, f64 float64) (float64, int, error) {
	if i >= len(data) {
		return 0, i, syntaxErr(i, reachedEndScanningDigit, nil)
	}
	b := data[i]
	if b < '0' || b > '9' {
		return f64, i, syntaxErr(i, needAtLeastOneDigit, nil)
	}
	for ; i < len(data); i++ {
		d := data[i]
		if d < '0' || d > '9' {
			return f64, i, nil
		}
		f64 = f64*10 + float64(d-'0')
	}
	return f64, i, nil
}

func scanFractionalPart(data []byte, i int) (float64, int, error) {
	if i >= len(data) {
		return 0, i, syntaxErr(i, reachedEndScanningDigit, nil)
	}
	d := data[i]
	if d < '0' || d > '9' {
		return 0, i, syntaxErr(i, needAtLeastOneDigit, nil)
	}
	scale := float64(10.0)
	v := float64(d-'0') / scale
	i++
	for ; i < len(data); i++ {
		d = data[i]
		if d < '0' || d > '9' {
			return v, i, nil
		}
		scale *= 10.0
		v += float64(d-'0') / scale
	}
	return v, i, nil
}

// skipWhitespace advances i until a non-whitespace character is
// found.
func skipWhitespace(data []byte, i int) int {
	if i < 0 {
		panic(fmt.Sprintf("negative i=%v", i))
	}
	for ; i < len(data); i++ {
		b := data[i]
		if b != ' ' &&
			b != '\t' &&
			b != '\n' &&
			b != '\r' {
			return i
		}
	}
	return i
}
