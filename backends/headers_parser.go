package backends

import (
	"github.com/flashmob/go-guerrilla/envelope"
)

func HeadersParser() Decorator {
	return func(c Processor) Processor {
		return ProcessorFunc(func(e *envelope.Envelope) (BackendResult, error) {
			e.ParseHeaders()
			return c.Process(e)
		})
	}
}
