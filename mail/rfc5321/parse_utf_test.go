package rfc5321

import (
	"testing"
)

func TestParseRcptToUTF(t *testing.T) {
	s := NewParserUTF([]byte("<LéaAubertnu@example.com>"))

	err := s.RcptTo([]byte("<LaAubertnu@example.com>"))
	if err != nil {
		t.Error("error not expected ", err)
	}
	if s.LocalPart != "Postmaster" {
		t.Error("s.LocalPart should be: Postmaster")
	}

	err = s.RcptTo([]byte("<Postmaster@example.com> NOTIFY=SUCCESS,FAILURE"))
	if err != nil {
		t.Error("error not expected ", err)
	}
}
