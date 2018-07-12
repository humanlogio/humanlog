package rgbterm

import (
	"bytes"
	"testing"
)

func bufsForTest(s string) (inputBuf *bytes.Buffer, outputBuf *bytes.Buffer) {
	inputBuf = bytes.NewBufferString(s)
	outputBuf = &bytes.Buffer{}
	return
}

func TestInterpretNoEscapes(t *testing.T) {
	s := "No escapes here."
	input, output := bufsForTest("No escapes here.")

	cnt := 0

	subst := func(s string) []byte {
		cnt++
		return nil
	}

	err := interpret(input, output, subst)

	if err != nil {
		t.FailNow()
	}

	if cnt != 0 {
		t.FailNow()
	}

	if output.String() != s {
		t.FailNow()
	}
}

func TestInterpretOutputBrace(t *testing.T) {
	sin := "We have {{no} escapes here."
	sout := "We have {no} escapes here."
	input, output := bufsForTest(sin)

	cnt := 0

	subst := func(s string) []byte {
		cnt++
		return []byte{}
	}

	err := interpret(input, output, subst)

	if err != nil {
		t.FailNow()
	}

	if cnt != 0 {
		t.FailNow()
	}

	if output.String() != sout {
		t.FailNow()
	}
}

func TestInterpretEmptyReplace(t *testing.T) {
	sin := "We have {some} escapes here."
	sout := "We have  escapes here."
	input, output := bufsForTest(sin)

	cnt := 0

	subst := func(s string) []byte {
		cnt++
		return []byte{}
	}

	err := interpret(input, output, subst)
	if err != nil {
		return
	}

	if cnt != 1 {
		t.FailNow()
	}

	if output.String() != sout {
		t.FailNow()
	}
}

func TestInterpretNilReplace(t *testing.T) {
	sin := "We have {some} escapes here."
	sout := "We have  escapes here."
	input, output := bufsForTest(sin)

	cnt := 0

	subst := func(s string) []byte {
		if s != "some" {
			t.FailNow()
		}
		cnt++
		return nil
	}

	err := interpret(input, output, subst)

	if err != nil {
		return
	}

	if cnt != 1 {
		t.FailNow()
	}

	if output.String() != sout {
		t.FailNow()
	}
}

func TestInterpretSomeReplaces(t *testing.T) {
	sin := "We have {some} escapes {more} here."
	sout := "We have more escapes more here."
	input, output := bufsForTest(sin)

	cnt := 0

	subst := func(s string) []byte {
		if s != "some" && s != "more" {
			t.FailNow()
		}
		cnt++
		return []byte("more")
	}

	err := interpret(input, output, subst)

	if err != nil {
		return
	}

	if cnt != 2 {
		t.FailNow()
	}

	if output.String() != sout {
		t.FailNow()
	}
}

func TestInterpretReplaceTooLong(t *testing.T) {
	sin := "We have {#0a0a0a,#0a0a0a,}"
	input, output := bufsForTest(sin)

	cnt := 0

	subst := func(s string) []byte {
		cnt++
		return []byte{}
	}

	err := interpret(input, output, subst)

	if err != nil {
		return
	}

	if cnt != 0 {
		t.Log("Count is", cnt)
		t.FailNow()
	}

	if output.String() != sin {
		t.Log("Str is", output.String())
		t.FailNow()
	}
}

func TestParseEmptyEscape(t *testing.T) {
	fg, bg := parseEscape("")

	if fg != nil || bg != nil {
		t.FailNow()
	}
}

func TestParseInvalidEscape(t *testing.T) {
	fg, bg := parseEscape("invl")

	if fg != nil || bg != nil {
		t.FailNow()
	}
}

func TestParseFgEscape(t *testing.T) {
	fg, bg := parseEscape("#0a0501")

	if fg[0] != 0xa || fg[1] != 0x5 || fg[2] != 0x1 {
		t.FailNow()
	}

	if bg != nil {
		t.FailNow()
	}
}

func TestParseFgBgEscape(t *testing.T) {
	fg, bg := parseEscape("#0a0501,#fffefd")

	if fg[0] != 0xa || fg[1] != 0x5 || fg[2] != 0x1 {
		t.FailNow()
	}

	if bg[0] != 0xff || bg[1] != 0xfe || bg[2] != 0xfd {
		t.FailNow()
	}
}

func TestParseBgEscape(t *testing.T) {
	fg, bg := parseEscape(",#fffefd")

	if fg != nil {
		t.FailNow()
	}

	if bg[0] != 0xff || bg[1] != 0xfe || bg[2] != 0xfd {
		t.FailNow()
	}
}
