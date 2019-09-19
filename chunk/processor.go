package chunk

import (
	"errors"
	"fmt"
	"net"

	"github.com/flashmob/go-guerrilla/backends"
	"github.com/flashmob/go-guerrilla/mail"
	"github.com/flashmob/go-guerrilla/mail/mime"
)

// ----------------------------------------------------------------------------------
// Processor Name: ChunkSaver
// ----------------------------------------------------------------------------------
// Description   : Takes the stream and saves it in chunks. Chunks are split on the
//               : chunksaver_chunk_size config setting, and also at the end of MIME parts,
//               : and after a header. This allows for basic de-duplication: we can take a
//               : hash of each chunk, then check the database to see if we have it already.
//               : We don't need to write it to the database, but take the reference of the
//               : previously saved chunk and only increment the reference count.
//               : The rationale to put headers and bodies into separate chunks is
//               : due to headers often containing more unique data, while the bodies are
//               : often duplicated, especially for messages that are CC'd or forwarded
// ----------------------------------------------------------------------------------
// Requires      : "mimeanalyzer" stream processor to be enabled before it
// ----------------------------------------------------------------------------------
// Config Options: chunksaver_chunk_size - maximum chunk size, in bytes
// --------------:-------------------------------------------------------------------
// Input         : e.Values["MimeParts"] Which is of type *[]*mime.Part, as populated by "mimeanalyzer"
// ----------------------------------------------------------------------------------
// Output        :
// ----------------------------------------------------------------------------------

func init() {
	backends.Streamers["chunksaver"] = func() *backends.StreamDecorator {
		return Chunksaver()
	}
}

type ChunkSaverConfig struct {
	// ChunkMaxBytes controls the maximum buffer size for saving
	// 16KB default.
	ChunkMaxBytes int    `json:"chunksaver_chunk_size,omitempty"`
	StorageEngine string `json:"chunksaver_storage_engine,omitempty"`
	CompressLevel int    `json:"chunksaver_compress_level,omitempty"`
}

//const chunkMaxBytes = 1024 * 16 // 16Kb is the default, change using chunksaver_chunk_size config setting
/**
*
 * A chunk ends ether:
 * after xKB or after end of a part, or end of header
 *
 * - buffer first chunk
 * - if didn't receive first chunk for more than x bytes, save normally
 *
*/
func Chunksaver() *backends.StreamDecorator {

	sd := &backends.StreamDecorator{}
	sd.Decorate =
		func(sp backends.StreamProcessor, a ...interface{}) backends.StreamProcessor {
			var (
				envelope    *mail.Envelope
				chunkBuffer *ChunkedBytesBufferMime
				msgPos      uint
				database    ChunkSaverStorage
				written     int64

				// just some headers from the first mime-part
				subject string
				to      string
				from    string

				progress int // tracks which mime parts were processed
			)

			var config *ChunkSaverConfig
			// optional dependency injection
			for i := range a {
				if db, ok := a[i].(ChunkSaverStorage); ok {
					database = db
				}
				if buff, ok := a[i].(*ChunkedBytesBufferMime); ok {
					chunkBuffer = buff
				}
			}

			backends.Svc.AddInitializer(backends.InitializeWith(func(backendConfig backends.BackendConfig) error {

				configType := backends.BaseConfig(&ChunkSaverConfig{})
				bcfg, err := backends.Svc.ExtractConfig(backendConfig, configType)
				if err != nil {
					return err
				}
				config = bcfg.(*ChunkSaverConfig)
				if chunkBuffer == nil {
					chunkBuffer = NewChunkedBytesBufferMime()
				}
				// configure storage if none was injected
				if database == nil {
					if config.StorageEngine == "memory" {
						db := new(ChunkSaverMemory)
						db.CompressLevel = config.CompressLevel
						database = db
					} else {
						db := new(ChunkSaverSQL)
						database = db
					}
				}
				err = database.Initialize(backendConfig)
				if err != nil {
					return err
				}
				// configure the chunks buffer
				if config.ChunkMaxBytes > 0 {
					chunkBuffer.CapTo(config.ChunkMaxBytes)
				} else {
					chunkBuffer.CapTo(chunkMaxBytes)
				}
				chunkBuffer.SetDatabase(database)

				return nil
			}))

			backends.Svc.AddShutdowner(backends.ShutdownWith(func() error {
				err := database.Shutdown()
				return err
			}))

			sd.Open = func(e *mail.Envelope) error {
				// create a new entry & grab the id
				written = 0
				progress = 0
				var ip net.IPAddr
				if ret := net.ParseIP(e.RemoteIP); ret != nil {
					ip = net.IPAddr{IP: ret}
				}
				mid, err := database.OpenMessage(
					e.MailFrom.String(),
					e.Helo,
					e.RcptTo[0].String(),
					ip,
					e.MailFrom.String(),
					e.TLS)
				if err != nil {
					return err
				}
				e.Values["messageID"] = mid
				envelope = e
				return nil
			}

			sd.Close = func() (err error) {
				err = chunkBuffer.Flush()
				if err != nil {
					// TODO we could delete the half saved message here
					return err
				}
				defer chunkBuffer.Reset()
				if mid, ok := envelope.Values["messageID"].(uint64); ok {
					err = database.CloseMessage(
						mid,
						written,
						&chunkBuffer.Info,
						subject,
						envelope.QueuedId,
						to,
						from,
					)
					if err != nil {
						return err
					}
				}
				return nil
			}

			fillVars := func(parts *[]*mime.Part, subject, to, from string) (string, string, string) {
				if len(*parts) > 0 {
					if subject == "" {
						if val, ok := (*parts)[0].Headers["Subject"]; ok {
							subject = val[0]
						}
					}
					if to == "" {
						if val, ok := (*parts)[0].Headers["To"]; ok {
							addr, err := mail.NewAddress(val[0])
							if err == nil {
								to = addr.String()
							}
						}
					}
					if from == "" {
						if val, ok := (*parts)[0].Headers["From"]; ok {
							addr, err := mail.NewAddress(val[0])
							if err == nil {
								from = addr.String()
							}
						}
					}

				}
				return subject, to, from
			}

			return backends.StreamProcessWith(func(p []byte) (count int, err error) {
				if envelope.Values == nil {
					return count, errors.New("no message headers found")
				}
				if parts, ok := envelope.Values["MimeParts"].(*[]*mime.Part); ok && len(*parts) > 0 {
					var pos int

					subject, to, from = fillVars(parts, subject, to, from)
					offset := msgPos
					chunkBuffer.CurrentPart((*parts)[0])
					for i := progress; i < len(*parts); i++ {
						part := (*parts)[i]

						// break chunk on new part
						if part.StartingPos > 0 && part.StartingPos > msgPos {
							count, _ = chunkBuffer.Write(p[pos : part.StartingPos-offset])
							written += int64(count)

							err = chunkBuffer.Flush()
							if err != nil {
								return count, err
							}
							chunkBuffer.CurrentPart(part)
							fmt.Println("->N")
							pos += count
							msgPos = part.StartingPos
						}
						// break chunk on header
						if part.StartingPosBody > 0 && part.StartingPosBody >= msgPos {
							count, _ = chunkBuffer.Write(p[pos : part.StartingPosBody-offset])
							written += int64(count)

							err = chunkBuffer.Flush()
							if err != nil {
								return count, err
							}
							chunkBuffer.CurrentPart(part)
							fmt.Println("->H")
							pos += count
							msgPos = part.StartingPosBody
						}
						// if on the latest (last) part, and yet there is still data to be written out
						if len(*parts)-1 == i && len(p)-1 > pos {
							count, _ = chunkBuffer.Write(p[pos:])
							written += int64(count)
							pos += count
							msgPos += uint(count)
						}
						// if there's no more data
						if pos >= len(p) {
							break
						}
					}
					if len(*parts) > 2 {
						progress = len(*parts) - 2 // skip to 2nd last part, assume previous parts are already processed
					}
				}
				return sp.Write(p)
			})
		}
	return sd
}