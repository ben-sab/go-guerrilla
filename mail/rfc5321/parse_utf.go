package rfc5321

import (
	"errors"
	"fmt"
)

type AddressParser interface {
	RcptTo(input []byte) (err error)
	MailFrom(input []byte) (err error)
	Reset()
}

type ParserUTF struct {
	*Parser
}

func NewParserUTF(buf []byte) *ParserUTF {
	s := &ParserUTF{
		new(Parser),
	}
	s.buf = buf
	s.pos = -1
	return s
}

//MailFrom accepts the following syntax: Reverse-path [SP Mail-parameters] CRLF
func (s *ParserUTF) MailFrom(input []byte) (err error) {
	s.set(input)
	if err := s.reversePath(); err != nil {
		return err
	}
	s.next()
	if p := s.next(); p == ' ' {
		// parse Rcpt-parameters
		// The optional <mail-parameters> are associated with negotiated SMTP
		//  service extensions
		if tup, err := s.parameters(); err != nil {
			return errors.New("param parse error")
		} else if len(tup) > 0 {
			s.PathParams = tup
		}
	}
	return nil
}

//RcptTo accepts the following syntax: ( "<Postmaster@" Domain ">" / "<Postmaster>" /
//                  Forward-path ) [SP Rcpt-parameters] CRLF
func (s *ParserUTF) RcptTo(input []byte) (err error) {
	s.set(input)
	if err := s.forwardPath(); err != nil {
		return err
	}
	s.next()
	if p := s.next(); p == ' ' {
		// parse Rcpt-parameters
		if tup, err := s.parameters(); err != nil {
			return errors.New("param parse error")
		} else if len(tup) > 0 {
			s.PathParams = tup
		}
	}
	return nil
}

func (s *ParserUTF) isAtext(c byte) bool {
	fmt.Println("HEY HO")
	if ('0' <= c && c <= '9') ||
		('A' <= c && c <= 'z') ||
		c == '!' || c == '#' ||
		c == '$' || c == '%' ||
		c == '&' || c == '\'' ||
		c == '*' || c == '+' ||
		c == '-' || c == '/' ||
		c == '=' || c == '?' ||
		c == '^' || c == '_' ||
		c == '`' || c == '{' ||
		c == '|' || c == '}' ||
		c == '~' {
		return true
	}
	return false
}

// func isLetDig(c byte) bool {
// 	if ('0' <= c && c <= '9') ||
// 		('A' <= c && c <= 'z') {
// 		return true
// 	}
// 	return false
// }
