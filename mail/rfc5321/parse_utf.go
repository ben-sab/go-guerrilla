package rfc5321

type AddressParser interface {
	RcptTo(input []byte) (err error)
	MailFrom(input []byte) (err error)
	Reset()
}

type ParserUTF struct {
	parser Parser
}
